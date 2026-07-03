package dev.opcode42.core.data

import dev.opcode42.core.model.AgentInfo
import dev.opcode42.core.model.AppEvent
import dev.opcode42.core.model.CommandInfo
import dev.opcode42.core.model.FilePartInput
import dev.opcode42.core.model.Message
import dev.opcode42.core.model.ModelRef
import dev.opcode42.core.model.Part
import dev.opcode42.core.model.PermissionRequest
import dev.opcode42.core.model.ProvidersResponse
import dev.opcode42.core.model.QuestionRequest
import dev.opcode42.core.model.Session
import dev.opcode42.core.model.SnapshotFileDiff
import dev.opcode42.core.model.VcsInfo
import dev.opcode42.core.sdk.Opcode42Client
import dev.opcode42.core.store.AppStore
import dev.opcode42.core.store.ConnectionState
import dev.opcode42.core.store.OptimisticMessage
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.withContext
import javax.inject.Inject
import javax.inject.Singleton

/** Everything one chat screen renders, projected from app state without leaking the store. */
data class ChatSnapshot(
    val session: Session? = null,
    val messages: List<Message> = emptyList(),
    val parts: Map<String, List<Part>> = emptyMap(),
    val optimistic: List<OptimisticMessage> = emptyList(),
    val permissions: List<PermissionRequest> = emptyList(),
    val questions: List<QuestionRequest> = emptyList(),
    val status: String = "idle",
    val diffs: Map<String, List<SnapshotFileDiff>> = emptyMap(),
)

/**
 * Data-layer owner of a chat session's messages, parts, diffs, and the prompt-send path.
 * Owns the optimistic-send add/rollback and the idempotent diff fetch that previously lived in
 * `ChatViewModel`. Reads are projected from the [AppStore]; the SSE stream feeds the same store.
 */
interface ChatRepository {
    /** Reactive view of one session's chat state. */
    fun observe(sessionId: String): Flow<ChatSnapshot>

    /** Global connection state — the UI reloads messages on each (re)connect. */
    val connectionState: Flow<ConnectionState>

    suspend fun loadMessages(sessionId: String, directory: String?): Result<Unit>

    /** D1 — Fetch a subagent (child) session's message transcript and cache it for inline
     *  display in a SubAgentBlock. Returns the messages so the caller (ChatViewModel) can
     *  publish them into [childSessionMessages]. */
    suspend fun loadChildMessages(childSessionId: String): List<Message>

    /** Optimistically post [text], rolling the optimistic bubble back if the request fails. */
    suspend fun send(
        sessionId: String,
        text: String,
        directory: String?,
        attachments: List<FilePartInput>,
        model: ModelRef?,
        agent: String?,
    ): Result<Unit>

    /** Fetch the unified diff for a message. Idempotent: a message already loaded or in flight is a no-op. */
    suspend fun loadDiff(sessionId: String, messageId: String, directory: String): Result<Unit>

    suspend fun searchFiles(query: String, directory: String?): Result<List<String>>

    /** Current VCS branch info for [directory] — for the chat header. Failure (no `/vcs`) is a normal result. */
    suspend fun vcsInfo(directory: String): Result<VcsInfo>

    /** Working-tree changes for [directory] (the daemon's `git status`) — the CHANGES pane's
     *  net file list. Failure (no `/vcs`) is a normal result the caller treats as "no changes". */
    suspend fun vcsStatus(directory: String): Result<List<SnapshotFileDiff>>

    /** Working-tree changes for [directory] *with patches* (`/vcs/diff`) — the source the
     *  diff viewer renders. Heavier than [vcsStatus]; fetched lazily when a CHANGES row is tapped. */
    suspend fun vcsDiff(directory: String): Result<List<SnapshotFileDiff>>

    suspend fun listCommands(directory: String?): Result<List<CommandInfo>>
    suspend fun listProviders(directory: String?): Result<ProvidersResponse>
    suspend fun listAgents(directory: String?): Result<List<AgentInfo>>
    suspend fun runCommand(sessionId: String, name: String, arguments: String, directory: String?): Result<Unit>
}

@Singleton
class DefaultChatRepository @Inject constructor(
    private val client: Opcode42Client,
    private val store: AppStore,
) : ChatRepository {

    // Guards against duplicate in-flight diff fetches across concurrent collectors.
    private val diffInFlight = mutableSetOf<String>()

    override fun observe(sessionId: String): Flow<ChatSnapshot> = store.state.map { s ->
        val messages = s.messages[sessionId] ?: emptyList()
        // Project parts/diffs down to THIS session so an unrelated session's stream update
        // doesn't change this snapshot — which lets distinctUntilChanged actually elide it
        // (parts carry their own sessionID; diffs are keyed by this session's message ids).
        // Note: a message whose live-parts bucket is emptied by PartRemoved drops out here, so
        // effectiveParts falls back to the REST-loaded message.parts — the intended behavior.
        val messageIds = messages.mapTo(HashSet(messages.size)) { it.id }
        ChatSnapshot(
            session = s.sessions.firstOrNull { it.id == sessionId },
            messages = messages,
            parts = s.parts.filterValues { it.firstOrNull()?.sessionID == sessionId },
            optimistic = s.optimisticMessages[sessionId] ?: emptyList(),
            permissions = s.permissions[sessionId] ?: emptyList(),
            questions = s.questions[sessionId] ?: emptyList(),
            status = s.sessionStatus[sessionId] ?: "idle",
            diffs = s.diffs.filterKeys { it in messageIds },
        )
    }.distinctUntilChanged()

    override val connectionState: Flow<ConnectionState> =
        store.state.map { it.connectionState }.distinctUntilChanged()

    override suspend fun loadMessages(sessionId: String, directory: String?): Result<Unit> = resultOf {
        client.getMessages(sessionId, directory).forEach { store.dispatch(AppEvent.MessageUpdated(it)) }
    }

    override suspend fun loadChildMessages(childSessionId: String): List<Message> =
        client.getMessages(childSessionId, null)

    override suspend fun send(
        sessionId: String,
        text: String,
        directory: String?,
        attachments: List<FilePartInput>,
        model: ModelRef?,
        agent: String?,
    ): Result<Unit> {
        val optimisticId = if (text.isNotBlank()) store.addOptimistic(sessionId, text) else null
        return try {
            client.sendPrompt(sessionId, text, directory, attachments, model = model, agent = agent)
            Result.success(Unit)
        } catch (e: Exception) {
            // Roll the optimistic bubble back on any failure — including cancellation (VM cleared
            // mid-send). NonCancellable lets the suspending removeOptimistic complete even as the
            // coroutine unwinds; CancellationException is then re-thrown to honor cancellation.
            optimisticId?.let { withContext(NonCancellable) { store.removeOptimistic(sessionId, it) } }
            if (e is CancellationException) throw e
            Result.failure(e)
        }
    }

    override suspend fun loadDiff(sessionId: String, messageId: String, directory: String): Result<Unit> {
        val shouldLoad = synchronized(diffInFlight) {
            if (messageId in diffInFlight || store.state.value.diffs.containsKey(messageId)) {
                false
            } else {
                diffInFlight.add(messageId)
                true
            }
        }
        if (!shouldLoad) return Result.success(Unit)
        return try {
            resultOf { client.getSessionDiff(sessionId, messageId, directory) }
                .onSuccess { store.dispatch(AppEvent.SessionDiffLoaded(messageId, it)) }
                // Store an empty list on failure so the auto-loader doesn't retry this message forever.
                .onFailure { store.dispatch(AppEvent.SessionDiffLoaded(messageId, emptyList())) }
                .map { }
        } finally {
            // Always release the in-flight guard — even on cancellation, where resultOf re-throws
            // before the chain above runs. Since diffInFlight lives on this @Singleton, a leaked
            // entry would poison the messageId for the whole process (the original per-VM set was
            // discarded with the ViewModel, so this is stricter than main on purpose).
            synchronized(diffInFlight) { diffInFlight.remove(messageId) }
        }
    }

    override suspend fun searchFiles(query: String, directory: String?): Result<List<String>> =
        resultOf { client.findFiles(query, directory) }

    override suspend fun vcsInfo(directory: String): Result<VcsInfo> =
        resultOf { client.getVcsInfo(directory) }

    override suspend fun vcsStatus(directory: String): Result<List<SnapshotFileDiff>> =
        resultOf { client.getVcsStatus(directory) }

    override suspend fun vcsDiff(directory: String): Result<List<SnapshotFileDiff>> =
        resultOf { client.getVcsDiff(directory) }

    override suspend fun listCommands(directory: String?): Result<List<CommandInfo>> =
        resultOf { client.listCommands(directory) }

    override suspend fun listProviders(directory: String?): Result<ProvidersResponse> =
        resultOf { client.listProviders(directory) }

    override suspend fun listAgents(directory: String?): Result<List<AgentInfo>> =
        resultOf { client.listAgents(directory) }

    override suspend fun runCommand(
        sessionId: String,
        name: String,
        arguments: String,
        directory: String?,
    ): Result<Unit> = resultOf { client.runCommand(sessionId, name, arguments, directory) }
}
