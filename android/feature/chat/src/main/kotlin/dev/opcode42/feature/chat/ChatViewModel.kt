package dev.opcode42.feature.chat

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.opcode42.core.data.ChatRepository
import dev.opcode42.core.data.SessionRepository
import dev.opcode42.core.data.toUserMessage
import dev.opcode42.core.model.*
import dev.opcode42.core.store.ConnectionState
import dev.opcode42.core.store.OptimisticMessage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
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

/** One-shot UI events consumed once by the screen (e.g. a transient error snackbar). */
sealed interface ChatEvent {
    data class ShowError(val message: String) : ChatEvent
}

data class ChatUiState(
    val session: Session? = null,
    val messages: List<Message> = emptyList(),
    val parts: Map<String, List<Part>> = emptyMap(),
    val diffs: Map<String, List<SnapshotFileDiff>> = emptyMap(),
    /** Working-tree changes (the daemon's `git status`) for the session's directory — the
     *  net changed-files the CHANGES pane shows, refreshed when the session goes idle. */
    val changedFiles: List<SnapshotFileDiff> = emptyList(),
    val optimisticMessages: List<OptimisticMessage> = emptyList(),
    val pendingPermissions: List<PermissionRequest> = emptyList(),
    val pendingQuestions: List<QuestionRequest> = emptyList(),
    val sessionStatus: String = "idle",
    val todos: List<TodoItem> = emptyList(),
    /** Agent name driving the status-strip mode chip (e.g. "build", "plan"). */
    val agentMode: String? = null,
    val modelID: String? = null,
    val providerID: String? = null,
    /** Current VCS branch of the session's directory, for the header subtitle; null = unknown/not a repo. */
    val branch: String? = null,
    /** Live context-window occupancy: the most recent assistant turn's token usage
     *  (NOT the session's cumulative total, which grows unbounded across turns). */
    val contextTokens: TokenUsage? = null,
    /** Real context-window size of the model that produced [contextTokens], from the
     *  models.dev catalog via GET /provider (opencode `Model.limit.context`). null when
     *  unknown — the gauge then shows the token count without a denominator/percentage. */
    val contextLimit: Int? = null,
    val isLoading: Boolean = false,
    val isSending: Boolean = false,
)

/**
 * Sentinel session id for the lazy "new session" draft. A draft holds no server session:
 * we defer `POST /session` until the user actually sends the first prompt, so abandoned
 * drafts never leave empty "New session" rows in the list. On first send the session is
 * created, the prompt is posted to it, and navigation swaps the draft route for the real
 * session (see [ChatViewModel.sendPrompt] and the nav graph's NewChat route).
 */
const val DRAFT_SESSION_ID = "new"

@HiltViewModel
class ChatViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val chatRepo: ChatRepository,
    private val sessionRepo: SessionRepository,
) : ViewModel() {

    private val sessionId: String = checkNotNull(savedStateHandle["sessionId"])

    /** True for the lazy "new session" draft — no server session exists yet. */
    private val isDraft: Boolean = sessionId == DRAFT_SESSION_ID

    // Transient UI flags not derived from the store — combined into uiState below.
    private val _isSending = MutableStateFlow(false)
    private val _isLoading = MutableStateFlow(false)

    /** Current git branch of the session directory; fetched once when the directory is known. */
    private val _branch = MutableStateFlow<String?>(null)

    /** Providers + their models for the picker (loaded once); also feeds the context
     *  gauge's real window size. Declared above [uiState] because the combine reads it. */
    private val _providers = MutableStateFlow<List<ProviderInfo>>(emptyList())
    val providers: StateFlow<List<ProviderInfo>> = _providers.asStateFlow()

    /** Working-tree changes (`git status`) for the session directory; refreshed on idle. */
    private val _changedFiles = MutableStateFlow<List<SnapshotFileDiff>>(emptyList())

    /** Lazily-fetched `/vcs/diff` patches (the heavier sibling of [_changedFiles]) for the diff
     *  viewer: fetched once on the first tapped row, reused for later taps, and invalidated on each
     *  idle refresh so a finished turn re-fetches. Guarded by [vcsDiffLock] for the read-or-fetch. */
    private var vcsDiffCache: List<SnapshotFileDiff>? = null
    private val vcsDiffLock = Mutex()

    /** One-shot events (snackbars). BUFFERED + trySend so emitting never suspends or blocks. */
    private val _events = Channel<ChatEvent>(Channel.BUFFERED)
    val events = _events.receiveAsFlow()

    val uiState: StateFlow<ChatUiState> =
        // Six inputs, but `combine` is only typed up to five — pair the last two (both plain
        // state flows) into one and destructure them in the lambda.
        combine(
            chatRepo.observe(sessionId), _isSending, _isLoading, _branch,
            combine(_providers, _changedFiles) { providers, changedFiles -> providers to changedFiles },
        ) { snap, sending, loading, branch, (providers, changedFiles) ->
            val messages = snap.messages
            // Status-strip context comes from the most recent assistant turn that
            // carries a model/agent (the live "what's running" state).
            val lastModelled = messages.lastOrNull { it.role == "assistant" && it.modelID != null }
            // Context = the latest assistant turn that has produced output, matching
            // opencode's gauge-selection predicate (sidebar/context.tsx:20,
            // Context gauge: the last assistant message with token usage. Relaxed from
            // `output > 0` to `tokens != null` so the gauge populates immediately on session
            // switch from the session's history (the last turn's footprint) instead of
            // blanking until a new turn produces output. On a draft (no messages) the gauge
            // stays null — the UI shows 0/limit using the default model's limit.
            val contextMsg = messages.lastOrNull {
                it.role == "assistant" && it.tokens != null
            }
            ChatUiState(
                session = snap.session,
                messages = messages,
                parts = snap.parts,
                diffs = snap.diffs,
                changedFiles = changedFiles,
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
                branch = branch,
                contextTokens = contextMsg?.tokens,
                // Real window size of THAT turn's model, looked up in the providers
                // catalog by the message's provider/model (opencode sidebar/context.tsx:30,33).
                // Raw limit.context; null when the model isn't in the catalog — the gauge
                // then drops the denominator rather than inventing one.
                contextLimit = contextMsg?.let { m ->
                    providers.find { it.id == m.providerID }?.models?.get(m.modelID)?.limit?.context
                }?.takeIf { it > 0.0 }?.toInt(),
                isLoading = loading,
                isSending = sending,
            )
        }
            // Run the combine transform (esp. extractTodos' O(messages·parts) JSON scan) off the
            // main thread — it fires on every SSE delta while a turn streams.
            .flowOn(Dispatchers.Default)
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ChatUiState())

    private val directory: String? get() = uiState.value.session?.directory

    /** Slash commands available for this session's directory (loaded once). */
    private val _commands = MutableStateFlow<List<CommandInfo>>(emptyList())
    val commands: StateFlow<List<CommandInfo>> = _commands.asStateFlow()

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

    /**
     * Emits the real session id once a draft's first prompt has created the session, so the
     * UI navigates from the draft route to the real session (pushing the chat on top of the
     * home draft). A Channel gives true one-shot delivery (mirrors [events]).
     */
    private val _navigateToSession = Channel<String>(Channel.BUFFERED)
    val navigateToSession = _navigateToSession.receiveAsFlow()

    init {
        if (isDraft) {
            // A draft has no server session yet: nothing to load, no SSE directory, no diffs.
            // Load the pickers against the daemon default directory so a model/agent can be
            // chosen before the first prompt creates the session.
            loadPickers(null)
        } else {
            loadMessages()
            // Load slash commands / providers / agents once the directory is known.
            viewModelScope.launch {
                val dir = uiState.first { it.session?.directory != null }.session?.directory
                loadPickers(dir)
            }
            // Fetch the directory's VCS branch for the header subtitle (once dir is known).
            // Best-effort: backends without /vcs (the Go daemon currently 501s) or non-repo
            // directories leave the branch unshown.
            viewModelScope.launch {
                val dir = uiState.first { it.session?.directory != null }.session?.directory ?: return@launch
                chatRepo.vcsInfo(dir).onSuccess { info ->
                    _branch.value = info.branch?.takeIf { it.isNotBlank() }
                }
            }
            // Working-tree changes for the CHANGES pane: fetch once the directory is known,
            // then refresh whenever the session settles to idle (a finished turn may have
            // edited files). Best-effort — a backend without /vcs leaves the list empty.
            viewModelScope.launch {
                val dir = uiState.first { it.session?.directory != null }.session?.directory ?: return@launch
                suspend fun refresh() {
                    // Drop any cached patches so the next diff-viewer tap re-fetches this turn's edits.
                    vcsDiffLock.withLock { vcsDiffCache = null }
                    chatRepo.vcsStatus(dir).onSuccess { _changedFiles.value = it }
                }
                refresh()
                chatRepo.observe(sessionId)
                    .map { it.status }
                    .distinctUntilChanged()
                    .filter { it == "idle" }
                    .collect { refresh() }
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
    }

    /** Load slash commands, providers, and agents for [dir] (null = daemon default). */
    private fun loadPickers(dir: String?) {
        viewModelScope.launch {
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
    }

    private fun loadMessages() {
        viewModelScope.launch {
            _isLoading.value = true
            try {
                chatRepo.loadMessages(sessionId, directory)
                    .onFailure { android.util.Log.e("ChatVM", "loadMessages failed", it) }
            } finally {
                _isLoading.value = false
            }
        }
    }

    /** Log a failed user action and surface it as a one-shot snackbar event. */
    private fun emitError(action: String, cause: Throwable) {
        android.util.Log.w("ChatVM", "$action failed", cause)
        _events.trySend(ChatEvent.ShowError(cause.toUserMessage()))
    }

    /**
     * A7/C5 — Optimistic prompt submit with optional file attachments. On a draft this is the
     * lazy-creation point: create the session first, post the prompt to it, then signal
     * navigation to the real session — so an abandoned draft never persists an empty session.
     */
    fun sendPrompt(text: String, attachments: List<FilePartInput> = emptyList()) {
        viewModelScope.launch {
            // Idempotency: ignore a second send while one is in flight, so a double-tap on a
            // draft can't create two sessions (the second would be an orphaned empty one).
            if (_isSending.value) return@launch
            _isSending.value = true
            try {
                if (isDraft) {
                    val created = sessionRepo.create(directory = null).getOrElse {
                        emitError("create session", it)
                        return@launch
                    }
                    chatRepo.send(created.id, text, created.directory, attachments, _selectedModel.value, _selectedAgent.value)
                        .onFailure { emitError("send", it) }
                    _navigateToSession.trySend(created.id)
                } else {
                    chatRepo.send(sessionId, text, directory, attachments, _selectedModel.value, _selectedAgent.value)
                        .onFailure { emitError("send", it) }
                }
            } finally {
                _isSending.value = false
            }
        }
    }

    /** @-mention picker — fuzzy file search in the session directory. */
    suspend fun searchFiles(query: String): List<String> =
        chatRepo.searchFiles(query, directory)
            .onFailure { android.util.Log.w("ChatVM", "findFiles failed", it) }
            .getOrDefault(emptyList())

    /**
     * The patch for one CHANGES file, for the diff viewer. Fetches `/vcs/diff` for the session
     * directory once (under [vcsDiffLock]) and caches the result, returning the entry whose `file`
     * matches; later taps reuse the cache until an idle refresh clears it. Best-effort: a backend
     * without `/vcs` yields an empty list, so the caller sees null and shows "Empty diff."
     */
    suspend fun fileDiff(file: String): SnapshotFileDiff? {
        val dir = directory ?: return null
        val diffs = vcsDiffLock.withLock {
            vcsDiffCache ?: chatRepo.vcsDiff(dir).getOrDefault(emptyList()).also { vcsDiffCache = it }
        }
        // The CHANGES row's [file] was relativized to [dir] for display; /vcs/diff paths carry no
        // relativity guarantee, so relativize each candidate the same way before matching (a no-op
        // for opencode, which already returns repo-relative paths — kept for a divergent backend).
        val base = dir.removeSuffix("/")
        fun relToDir(p: String?): String? =
            if (p != null && p.startsWith("$base/")) p.removePrefix("$base/") else p
        return diffs.firstOrNull { relToDir(it.file) == file }
    }

    /** Slash palette — run a command by name with the trailing arguments. */
    fun runCommand(name: String, arguments: String) {
        viewModelScope.launch {
            chatRepo.runCommand(sessionId, name, arguments, directory)
                .onFailure { emitError("command", it) }
        }
    }

    /** Overflow → Fork: branch this session; [onForked] receives the new session id. */
    fun forkSession(onForked: (String) -> Unit) {
        viewModelScope.launch {
            sessionRepo.fork(sessionId)
                .onSuccess { onForked(it.id) }
                .onFailure { emitError("fork", it) }
        }
    }

    /** Overflow → Delete: remove this session; [onDeleted] runs on success (navigate back). */
    fun deleteSession(onDeleted: () -> Unit) {
        viewModelScope.launch {
            sessionRepo.delete(sessionId)
                .onSuccess { onDeleted() }
                .onFailure { emitError("delete", it) }
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
                .onFailure { emitError("rename", it) }
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
                .onFailure { emitError("archive", it) }
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
                .onFailure { emitError("summarize", it) }
        }
    }

    /** Overflow → Share: publish a link; the returned session (with share.url) updates the store. */
    fun shareSession() {
        viewModelScope.launch {
            sessionRepo.share(sessionId, directory)
                .onFailure { emitError("share", it) }
        }
    }

    /** Revoke the shared link; the returned session updates the store. */
    fun unshareSession() {
        viewModelScope.launch {
            sessionRepo.unshare(sessionId, directory)
                .onFailure { emitError("unshare", it) }
        }
    }

    /** Stop a running agent turn (composer stop button, shown while the session is busy). */
    fun abort() {
        // A draft has no server session to abort; the Stop button can briefly show while the
        // first prompt is creating the session, so ignore it rather than POST abort("new").
        if (isDraft) return
        viewModelScope.launch {
            sessionRepo.abort(sessionId, directory)
                .onFailure { emitError("abort", it) }
        }
    }

    /** A8 — Permission reply */
    fun replyPermission(requestId: String, allow: Boolean) {
        viewModelScope.launch {
            sessionRepo.replyPermission(requestId, allow).onFailure { emitError("reply", it) }
        }
    }

    /** A8 — Question reply */
    fun replyQuestion(requestId: String, answer: String) {
        viewModelScope.launch {
            sessionRepo.replyQuestion(requestId, answer).onFailure { emitError("reply", it) }
        }
    }

    fun rejectQuestion(requestId: String) {
        viewModelScope.launch {
            sessionRepo.rejectQuestion(requestId).onFailure { emitError("reject", it) }
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
