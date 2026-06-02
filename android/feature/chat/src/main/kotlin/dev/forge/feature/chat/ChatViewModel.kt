package dev.forge.feature.chat

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.forge.core.model.*
import dev.forge.core.network.SseManager
import dev.forge.core.sdk.ForgeClient
import dev.forge.core.store.AppStore
import dev.forge.core.store.OptimisticMessage
import dev.forge.core.store.ConnectionState
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import javax.inject.Inject

/** One todo as emitted by the `todowrite` tool (status: pending|in_progress|completed|cancelled). */
data class TodoItem(
    val content: String,
    val status: String,
    val priority: String? = null,
)

data class ChatUiState(
    val session: Session? = null,
    val messages: List<Message> = emptyList(),
    val parts: Map<String, List<Part>> = emptyMap(),
    val diffs: Map<String, List<SnapshotFileDiff>> = emptyMap(),
    val optimisticMessages: List<OptimisticMessage> = emptyList(),
    val pendingPermissions: List<PermissionRequest> = emptyList(),
    val pendingQuestions: List<QuestionRequest> = emptyList(),
    val sessionStatus: String = "idle",
    val todos: List<TodoItem> = emptyList(),
    /** Agent name driving the status-strip mode chip (e.g. "build", "plan"). */
    val agentMode: String? = null,
    val modelID: String? = null,
    val providerID: String? = null,
    val isLoading: Boolean = false,
    val isSending: Boolean = false,
)

@HiltViewModel
class ChatViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val client: ForgeClient,
    private val store: AppStore,
    private val sseManager: SseManager,
) : ViewModel() {

    private val sessionId: String = checkNotNull(savedStateHandle["sessionId"])

    // C4 — tracks in-flight diff fetches to prevent duplicate requests and infinite retry
    private val _diffInFlight = mutableSetOf<String>()

    val uiState: StateFlow<ChatUiState> = store.state
        .map { appState ->
            val session = appState.sessions.firstOrNull { it.id == sessionId }
            val messages = appState.messages[sessionId] ?: emptyList()
            // Status-strip context comes from the most recent assistant turn that
            // carries a model/agent (the live "what's running" state).
            val lastModelled = messages.lastOrNull { it.role == "assistant" && it.modelID != null }
            ChatUiState(
                session = session,
                messages = messages,
                parts = appState.parts,
                diffs = appState.diffs,
                optimisticMessages = appState.optimisticMessages[sessionId] ?: emptyList(),
                pendingPermissions = appState.permissions[sessionId] ?: emptyList(),
                pendingQuestions = appState.questions[sessionId] ?: emptyList(),
                sessionStatus = appState.sessionStatus[sessionId] ?: "idle",
                todos = extractTodos(messages, appState.parts),
                agentMode = lastModelled?.mode ?: lastModelled?.agent
                    ?: messages.lastOrNull { it.mode != null }?.mode
                    ?: messages.lastOrNull { it.agent != null }?.agent,
                modelID = lastModelled?.modelID,
                providerID = lastModelled?.providerID,
            )
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ChatUiState())

    private val directory: String? get() = uiState.value.session?.directory

    /** Slash commands available for this session's directory (loaded once). */
    private val _commands = MutableStateFlow<List<CommandInfo>>(emptyList())
    val commands: StateFlow<List<CommandInfo>> = _commands.asStateFlow()

    /** Providers + their models for the picker (loaded once). */
    private val _providers = MutableStateFlow<List<ProviderInfo>>(emptyList())
    val providers: StateFlow<List<ProviderInfo>> = _providers.asStateFlow()

    /** Selectable agents (primary/all modes) for the picker (loaded once). */
    private val _agents = MutableStateFlow<List<AgentInfo>>(emptyList())
    val agents: StateFlow<List<AgentInfo>> = _agents.asStateFlow()

    /** User's explicit model pick for upcoming prompts; null = use the server default. */
    private val _selectedModel = MutableStateFlow<ModelRef?>(null)
    val selectedModel: StateFlow<ModelRef?> = _selectedModel.asStateFlow()

    /** User's explicit agent pick for upcoming prompts; null = use the server default. */
    private val _selectedAgent = MutableStateFlow<String?>(null)
    val selectedAgent: StateFlow<String?> = _selectedAgent.asStateFlow()

    fun selectModel(model: ModelRef) { _selectedModel.value = model }

    fun selectAgent(name: String) { _selectedAgent.value = name }

    init {
        loadMessages()
        // Load slash commands once the directory is known.
        viewModelScope.launch {
            val dir = uiState.first { it.session?.directory != null }.session?.directory
            try {
                _commands.value = client.listCommands(dir)
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "listCommands failed", e)
            }
            try {
                val resp = client.listProviders(dir)
                // Only offer providers the daemon is actually authed against, so a picked
                // model can't fail the prompt. Fall back to all if `connected` is unreported.
                val connected = resp.connected.toSet()
                _providers.value =
                    if (connected.isEmpty()) resp.all else resp.all.filter { it.id in connected }
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "listProviders failed", e)
            }
            try {
                _agents.value = client.listAgents(dir).filter { it.isPrimary }
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "listAgents failed", e)
            }
        }
        // Subscribe to per-directory SSE exactly once, when the session's directory is known
        viewModelScope.launch {
            val dir = uiState.first { it.session?.directory != null }.session?.directory
            if (dir != null) sseManager.subscribeDirectory(dir)
        }
        // Reload messages after a reconnection (GlobalDisposed wipes state)
        viewModelScope.launch {
            store.state
                .map { it.connectionState }
                .distinctUntilChanged()
                .drop(1) // skip initial state; init already called loadMessages()
                .filter { it is ConnectionState.Connected }
                .collect { loadMessages() }
        }
        // C4 — Watch for new PatchParts and load diff content for each one not yet
        // fetched. Parts are keyed by messageID, so scan this session's messages
        // (live SSE parts supersede REST-loaded parts).
        viewModelScope.launch {
            store.state.collect { appState ->
                val dir = appState.sessions.firstOrNull { it.id == sessionId }?.directory ?: return@collect
                val msgs = appState.messages[sessionId] ?: return@collect
                msgs.forEach { msg ->
                    val parts = appState.parts[msg.id] ?: msg.parts
                    parts.filterIsInstance<PatchPart>()
                        .filter { it.messageID !in appState.diffs && it.messageID !in _diffInFlight }
                        .forEach { patch ->
                            _diffInFlight.add(patch.messageID)
                            loadDiff(patch.messageID, dir)
                        }
                }
            }
        }
    }

    private fun loadMessages() {
        viewModelScope.launch {
            try {
                val messages = client.getMessages(sessionId, directory)
                android.util.Log.d("ChatVM", "loadMessages: got ${messages.size} messages for $sessionId")
                messages.forEach { msg ->
                    android.util.Log.d("ChatVM", "  dispatch msg ${msg.id} role=${msg.role} parts=${msg.parts.size}")
                    store.dispatch(AppEvent.MessageUpdated(msg))
                }
            } catch (e: Exception) {
                android.util.Log.e("ChatVM", "loadMessages failed", e)
            }
        }
    }

    /** C4 — Fetch unified diff for a message and store in AppState */
    private fun loadDiff(messageId: String, directory: String) {
        viewModelScope.launch {
            try {
                val diffs = client.getSessionDiff(sessionId, messageId, directory)
                store.dispatch(AppEvent.SessionDiffLoaded(messageId, diffs))
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "loadDiff failed for $messageId", e)
                // Store empty list to prevent infinite retry on persistent failures
                store.dispatch(AppEvent.SessionDiffLoaded(messageId, emptyList()))
            } finally {
                _diffInFlight.remove(messageId)
            }
        }
    }

    /** A7/C5 — Optimistic prompt submit with optional file attachments */
    fun sendPrompt(text: String, attachments: List<FilePartInput> = emptyList()) {
        viewModelScope.launch {
            val optimisticId = if (text.isNotBlank()) store.addOptimistic(sessionId, text) else null
            try {
                client.sendPrompt(
                    sessionId, text, directory, attachments,
                    model = _selectedModel.value,
                    agent = _selectedAgent.value,
                )
            } catch (e: Exception) {
                optimisticId?.let { store.removeOptimistic(sessionId, it) }
            }
        }
    }

    /** @-mention picker — fuzzy file search in the session directory. */
    suspend fun searchFiles(query: String): List<String> =
        try {
            client.findFiles(query, directory)
        } catch (e: Exception) {
            android.util.Log.w("ChatVM", "findFiles failed", e)
            emptyList()
        }

    /** Slash palette — run a command by name with the trailing arguments. */
    fun runCommand(name: String, arguments: String) {
        viewModelScope.launch {
            try {
                client.runCommand(sessionId, name, arguments, directory)
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "runCommand failed", e)
            }
        }
    }

    /** Overflow → Fork: branch this session; [onForked] receives the new session id. */
    fun forkSession(onForked: (String) -> Unit) {
        viewModelScope.launch {
            try {
                val forked = client.forkSession(sessionId)
                onForked(forked.id)
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "forkSession failed", e)
            }
        }
    }

    /** Overflow → Delete: remove this session; [onDeleted] runs on success (navigate back). */
    fun deleteSession(onDeleted: () -> Unit) {
        viewModelScope.launch {
            try {
                client.deleteSession(sessionId)
                onDeleted()
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "deleteSession failed", e)
            }
        }
    }

    /** Effective model for ops that require one (summarize): explicit pick, else the last-run model. */
    private fun effectiveModel(): ModelRef? =
        _selectedModel.value ?: run {
            val p = uiState.value.providerID
            val m = uiState.value.modelID
            if (p != null && m != null) ModelRef(providerID = p, modelID = m) else null
        }

    /** Overflow → Rename: set the session title; the returned session updates the store. */
    fun renameSession(title: String) {
        val trimmed = title.trim()
        if (trimmed.isEmpty()) return
        viewModelScope.launch {
            try {
                store.dispatch(AppEvent.SessionUpdated(client.renameSession(sessionId, trimmed, directory)))
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "renameSession failed", e)
            }
        }
    }

    /** Overflow → Summarize: compact the context. No-op (logged) if no model is known yet. */
    fun summarize() {
        val model = effectiveModel() ?: run {
            android.util.Log.w("ChatVM", "summarize skipped: no model selected or running")
            return
        }
        viewModelScope.launch {
            try {
                client.summarizeSession(sessionId, model, directory)
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "summarize failed", e)
            }
        }
    }

    /** Overflow → Share: publish a link; the returned session (with share.url) updates the store. */
    fun shareSession() {
        viewModelScope.launch {
            try {
                store.dispatch(AppEvent.SessionUpdated(client.shareSession(sessionId, directory)))
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "shareSession failed", e)
            }
        }
    }

    /** Revoke the shared link; the returned session updates the store. */
    fun unshareSession() {
        viewModelScope.launch {
            try {
                store.dispatch(AppEvent.SessionUpdated(client.unshareSession(sessionId, directory)))
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "unshareSession failed", e)
            }
        }
    }

    /** Stop a running agent turn (composer stop button, shown while the session is busy). */
    fun abort() {
        viewModelScope.launch {
            try {
                client.abortSession(sessionId, directory)
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "abortSession failed", e)
            }
        }
    }

    /** A8 — Permission reply */
    fun replyPermission(requestId: String, allow: Boolean) {
        viewModelScope.launch {
            try {
                client.replyPermission(requestId, allow)
                store.dispatch(AppEvent.PermissionReplied(requestId))
            } catch (_: Exception) { }
        }
    }

    /** A8 — Question reply */
    fun replyQuestion(requestId: String, answer: String) {
        viewModelScope.launch {
            try {
                client.replyQuestion(requestId, answer)
                store.dispatch(AppEvent.QuestionReplied(requestId))
            } catch (_: Exception) { }
        }
    }

    fun rejectQuestion(requestId: String) {
        viewModelScope.launch {
            try {
                client.rejectQuestion(requestId)
                store.dispatch(AppEvent.QuestionRejected(requestId))
            } catch (_: Exception) { }
        }
    }
}

/**
 * Returns the todo list from the most recent `todowrite` tool call in the
 * session. Live SSE parts (keyed by messageID) supersede REST-loaded parts.
 * Messages are already ID-sorted, so the last match wins.
 */
private fun extractTodos(messages: List<Message>, parts: Map<String, List<Part>>): List<TodoItem> {
    var latest: List<TodoItem> = emptyList()
    for (msg in messages) {
        val msgParts = parts[msg.id] ?: msg.parts
        for (part in msgParts) {
            if (part !is ToolPart || part.tool != "todowrite") continue
            val input = when (val s = part.state) {
                is ToolStatePending -> s.input
                is ToolStateRunning -> s.input
                is ToolStateCompleted -> s.input
                is ToolStateError -> s.input
            } ?: continue
            val arr = input["todos"] as? JsonArray ?: continue
            val items = arr.mapNotNull { el ->
                val obj = el.jsonObject
                val content = obj["content"]?.jsonPrimitive?.content ?: return@mapNotNull null
                TodoItem(
                    content = content,
                    status = obj["status"]?.jsonPrimitive?.content ?: "pending",
                    priority = obj["priority"]?.jsonPrimitive?.content,
                )
            }
            if (items.isNotEmpty()) latest = items
        }
    }
    return latest
}
