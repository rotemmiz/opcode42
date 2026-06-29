package dev.opcode42.feature.chat

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.opcode42.core.data.ChatRepository
import dev.opcode42.core.data.SessionRepository
import dev.opcode42.core.model.*
import dev.opcode42.core.store.ConnectionState
import dev.opcode42.core.store.OptimisticMessage
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
    /** Live context-window occupancy: the most recent assistant turn's token usage
     *  (NOT the session's cumulative total, which grows unbounded across turns). */
    val contextTokens: TokenUsage? = null,
    val isLoading: Boolean = false,
    val isSending: Boolean = false,
)

@HiltViewModel
class ChatViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val chatRepo: ChatRepository,
    private val sessionRepo: SessionRepository,
) : ViewModel() {

    private val sessionId: String = checkNotNull(savedStateHandle["sessionId"])

    val uiState: StateFlow<ChatUiState> = chatRepo.observe(sessionId)
        .map { snap ->
            val messages = snap.messages
            // Status-strip context comes from the most recent assistant turn that
            // carries a model/agent (the live "what's running" state).
            val lastModelled = messages.lastOrNull { it.role == "assistant" && it.modelID != null }
            ChatUiState(
                session = snap.session,
                messages = messages,
                parts = snap.parts,
                diffs = snap.diffs,
                optimisticMessages = snap.optimistic,
                pendingPermissions = snap.permissions,
                pendingQuestions = snap.questions,
                sessionStatus = snap.status,
                todos = extractTodos(messages, snap.parts),
                agentMode = lastModelled?.mode ?: lastModelled?.agent
                    ?: messages.lastOrNull { it.mode != null }?.mode
                    ?: messages.lastOrNull { it.agent != null }?.agent,
                modelID = lastModelled?.modelID,
                providerID = lastModelled?.providerID,
                // Context = the latest assistant turn whose tokens are populated.
                // The daemon emits a zero-valued `tokens` block at turn START (before
                // counts are known), so we require a non-zero footprint — otherwise the
                // gauge would snap to 0% for the whole duration of each in-flight turn.
                contextTokens = messages.lastOrNull {
                    it.role == "assistant" && (it.tokens?.contextFootprint ?: 0L) > 0L
                }?.tokens,
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
        // Load slash commands / providers / agents once the directory is known.
        viewModelScope.launch {
            val dir = uiState.first { it.session?.directory != null }.session?.directory
            chatRepo.listCommands(dir)
                .onSuccess { _commands.value = it }
                .onFailure { android.util.Log.w("ChatVM", "listCommands failed", it) }
            chatRepo.listProviders(dir)
                .onSuccess { resp ->
                    // Only offer providers the daemon is actually authed against, so a picked
                    // model can't fail the prompt. Fall back to all if `connected` is unreported.
                    val connected = resp.connected.toSet()
                    _providers.value =
                        if (connected.isEmpty()) resp.all else resp.all.filter { it.id in connected }
                }
                .onFailure { android.util.Log.w("ChatVM", "listProviders failed", it) }
            chatRepo.listAgents(dir)
                .onSuccess { agents -> _agents.value = agents.filter { it.isPrimary } }
                .onFailure { android.util.Log.w("ChatVM", "listAgents failed", it) }
        }
        // Subscribe to per-directory SSE exactly once, when the session's directory is known
        viewModelScope.launch {
            val dir = uiState.first { it.session?.directory != null }.session?.directory
            if (dir != null) chatRepo.subscribeDirectory(dir)
        }
        // Reload messages after a reconnection (GlobalDisposed wipes state)
        viewModelScope.launch {
            chatRepo.connectionState
                .distinctUntilChanged()
                .drop(1) // skip initial state; init already called loadMessages()
                .filter { it is ConnectionState.Connected }
                .collect { loadMessages() }
        }
        // C4 — Watch for new PatchParts and load diff content for each one not yet fetched.
        // Parts are keyed by messageID (live SSE parts supersede REST-loaded parts). The repo's
        // loadDiff is idempotent and fire-and-forget; we still pre-filter already-loaded diffs
        // (from the snapshot) to avoid spawning no-op coroutines on every streaming delta.
        viewModelScope.launch {
            chatRepo.observe(sessionId).collect { snap ->
                val dir = snap.session?.directory ?: return@collect
                snap.messages.forEach { msg ->
                    val parts = snap.parts[msg.id] ?: msg.parts
                    parts.filterIsInstance<PatchPart>()
                        .filter { it.messageID !in snap.diffs }
                        .forEach { patch ->
                            viewModelScope.launch { chatRepo.loadDiff(sessionId, patch.messageID, dir) }
                        }
                }
            }
        }
    }

    private fun loadMessages() {
        viewModelScope.launch {
            chatRepo.loadMessages(sessionId, directory)
                .onFailure { android.util.Log.e("ChatVM", "loadMessages failed", it) }
        }
    }

    /** A7/C5 — Optimistic prompt submit with optional file attachments */
    fun sendPrompt(text: String, attachments: List<FilePartInput> = emptyList()) {
        viewModelScope.launch {
            chatRepo.send(sessionId, text, directory, attachments, _selectedModel.value, _selectedAgent.value)
                .onFailure { android.util.Log.w("ChatVM", "sendPrompt failed", it) }
        }
    }

    /** @-mention picker — fuzzy file search in the session directory. */
    suspend fun searchFiles(query: String): List<String> =
        chatRepo.searchFiles(query, directory)
            .onFailure { android.util.Log.w("ChatVM", "findFiles failed", it) }
            .getOrDefault(emptyList())

    /** Slash palette — run a command by name with the trailing arguments. */
    fun runCommand(name: String, arguments: String) {
        viewModelScope.launch {
            chatRepo.runCommand(sessionId, name, arguments, directory)
                .onFailure { android.util.Log.w("ChatVM", "runCommand failed", it) }
        }
    }

    /** Overflow → Fork: branch this session; [onForked] receives the new session id. */
    fun forkSession(onForked: (String) -> Unit) {
        viewModelScope.launch {
            sessionRepo.fork(sessionId)
                .onSuccess { onForked(it.id) }
                .onFailure { android.util.Log.w("ChatVM", "forkSession failed", it) }
        }
    }

    /** Overflow → Delete: remove this session; [onDeleted] runs on success (navigate back). */
    fun deleteSession(onDeleted: () -> Unit) {
        viewModelScope.launch {
            sessionRepo.delete(sessionId)
                .onSuccess { onDeleted() }
                .onFailure { android.util.Log.w("ChatVM", "deleteSession failed", it) }
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
            sessionRepo.rename(sessionId, trimmed, directory)
                .onFailure { android.util.Log.w("ChatVM", "renameSession failed", it) }
        }
    }

    /**
     * Overflow → Archive: set `time.archived`; the returned (archived) session updates the
     * store, dropping it from the active list. opencode has no un-archive path, so on success
     * we navigate back. [onArchived] runs only after the PATCH succeeds.
     */
    fun archiveSession(onArchived: () -> Unit) {
        viewModelScope.launch {
            sessionRepo.archive(sessionId, directory)
                .onSuccess { onArchived() }
                .onFailure { android.util.Log.w("ChatVM", "archiveSession failed", it) }
        }
    }

    /** Overflow → Summarize: compact the context. No-op (logged) if no model is known yet. */
    fun summarize() {
        val model = effectiveModel() ?: run {
            android.util.Log.w("ChatVM", "summarize skipped: no model selected or running")
            return
        }
        viewModelScope.launch {
            sessionRepo.summarize(sessionId, model, directory)
                .onFailure { android.util.Log.w("ChatVM", "summarize failed", it) }
        }
    }

    /** Overflow → Share: publish a link; the returned session (with share.url) updates the store. */
    fun shareSession() {
        viewModelScope.launch {
            sessionRepo.share(sessionId, directory)
                .onFailure { android.util.Log.w("ChatVM", "shareSession failed", it) }
        }
    }

    /** Revoke the shared link; the returned session updates the store. */
    fun unshareSession() {
        viewModelScope.launch {
            sessionRepo.unshare(sessionId, directory)
                .onFailure { android.util.Log.w("ChatVM", "unshareSession failed", it) }
        }
    }

    /** Stop a running agent turn (composer stop button, shown while the session is busy). */
    fun abort() {
        viewModelScope.launch {
            sessionRepo.abort(sessionId, directory)
                .onFailure { android.util.Log.w("ChatVM", "abortSession failed", it) }
        }
    }

    /** A8 — Permission reply */
    fun replyPermission(requestId: String, allow: Boolean) {
        viewModelScope.launch { sessionRepo.replyPermission(requestId, allow) }
    }

    /** A8 — Question reply */
    fun replyQuestion(requestId: String, answer: String) {
        viewModelScope.launch { sessionRepo.replyQuestion(requestId, answer) }
    }

    fun rejectQuestion(requestId: String) {
        viewModelScope.launch { sessionRepo.rejectQuestion(requestId) }
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
