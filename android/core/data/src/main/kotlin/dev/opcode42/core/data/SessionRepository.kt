package dev.opcode42.core.data

import dev.opcode42.core.model.AppEvent
import dev.opcode42.core.model.ModelRef
import dev.opcode42.core.model.PermissionRequest
import dev.opcode42.core.model.QuestionRequest
import dev.opcode42.core.model.Session
import dev.opcode42.core.sdk.Opcode42Client
import dev.opcode42.core.store.AppStore
import dev.opcode42.core.store.ConnectionState
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import javax.inject.Inject
import javax.inject.Singleton

/** The slice of app state the session list reads. Mirrors AppState without leaking the store. */
data class SessionsSnapshot(
    val sessions: List<Session> = emptyList(),
    val status: Map<String, String> = emptyMap(),
    val permissions: Map<String, List<PermissionRequest>> = emptyMap(),
    val questions: Map<String, List<QuestionRequest>> = emptyMap(),
)

/**
 * Data-layer owner of sessions and session-scoped interactions. Wraps the REST client and the
 * [AppStore] so ViewModels never touch either directly. Writes apply optimistically to the store
 * (the SSE stream later echoes the same change) and return a [Result] the UI can surface.
 */
interface SessionRepository {
    /** Reactive session-list state (sessions + status + pending permissions/questions). */
    val snapshot: Flow<SessionsSnapshot>

    /** SSE connection state (connected/connecting/disconnected/failed). */
    val connectionState: Flow<ConnectionState>

    /** Aggregate sessions across every project/directory the daemon knows. */
    suspend fun refreshAll(): Result<Unit>

    suspend fun create(directory: String?): Result<Session>
    suspend fun fork(sessionId: String): Result<Session>
    suspend fun rename(sessionId: String, title: String, directory: String? = null): Result<Session>
    suspend fun archive(sessionId: String, directory: String? = null): Result<Session>
    suspend fun delete(sessionId: String): Result<Unit>
    suspend fun share(sessionId: String, directory: String? = null): Result<Session>
    suspend fun unshare(sessionId: String, directory: String? = null): Result<Session>
    suspend fun summarize(sessionId: String, model: ModelRef, directory: String? = null): Result<Unit>
    suspend fun abort(sessionId: String, directory: String? = null): Result<Unit>

    suspend fun replyPermission(requestId: String, reply: String, message: String? = null): Result<Unit>
    suspend fun replyQuestion(requestId: String, answers: List<List<String>>): Result<Unit>
    suspend fun rejectQuestion(requestId: String): Result<Unit>

    /** G1 — Daemon version from `GET /global/health` (for the Settings About section). */
    suspend fun fetchDaemonVersion(): String?
}

@Singleton
class DefaultSessionRepository @Inject constructor(
    private val client: Opcode42Client,
    private val store: AppStore,
) : SessionRepository {

    override val snapshot: Flow<SessionsSnapshot> = store.state.map {
        SessionsSnapshot(it.sessions, it.sessionStatus, it.permissions, it.questions)
    }

    override val connectionState: Flow<ConnectionState> =
        store.state.map { it.connectionState }.distinctUntilChanged()

    /**
     * Enumerate `GET /project`, fan out `listSessions(dir)` per worktree + sandbox in parallel,
     * plus one no-directory call (covers the daemon's default/CWD project and the "all sessions"
     * case), then dedupe by id. Every call is wrapped so one unreachable directory never blanks
     * the whole list.
     */
    override suspend fun refreshAll(): Result<Unit> = resultOf {
        val projects = runCatching { client.listProjects() }.getOrDefault(emptyList())
        val dirs = projects
            .flatMap { listOf(it.worktree) + it.sandboxes }
            .filterNotNull()
            .filter { it != "/" } // root worktree of the synthetic "global" project
            .toSet()
        val results: List<Result<List<Session>>> = coroutineScope {
            val perDir = dirs.map { dir -> async { runCatching { client.listSessions(dir) } } }
            // The global (no-directory) call always runs: covers the daemon's default/CWD project.
            val global = async { runCatching { client.listSessions(null) } }
            perDir.awaitAll() + global.await()
        }
        // One unreachable directory must never blank the list — partial success still applies. But
        // if EVERY query failed, the daemon is unreachable/unconfigured; surface that so the UI can
        // offer a retry instead of a misleading "no sessions" empty state.
        if (results.all { it.isFailure }) throw results.firstNotNullOf { it.exceptionOrNull() }
        results.mapNotNull { it.getOrNull() }.flatten()
            .distinctBy { it.id }
            .forEach { store.dispatch(AppEvent.SessionUpdated(it)) }
    }

    override suspend fun create(directory: String?): Result<Session> = resultOf {
        client.createSession(directory).also { store.dispatch(AppEvent.SessionUpdated(it)) }
    }

    override suspend fun fork(sessionId: String): Result<Session> = resultOf {
        client.forkSession(sessionId).also { store.dispatch(AppEvent.SessionUpdated(it)) }
    }

    override suspend fun rename(sessionId: String, title: String, directory: String?): Result<Session> = resultOf {
        client.renameSession(sessionId, title, directory).also { store.dispatch(AppEvent.SessionUpdated(it)) }
    }

    override suspend fun archive(sessionId: String, directory: String?): Result<Session> = resultOf {
        client.archiveSession(sessionId, directory = directory).also { store.dispatch(AppEvent.SessionUpdated(it)) }
    }

    override suspend fun delete(sessionId: String): Result<Unit> = resultOf {
        client.deleteSession(sessionId)
        store.dispatch(AppEvent.SessionRemoved(sessionId))
    }

    override suspend fun share(sessionId: String, directory: String?): Result<Session> = resultOf {
        client.shareSession(sessionId, directory).also { store.dispatch(AppEvent.SessionUpdated(it)) }
    }

    override suspend fun unshare(sessionId: String, directory: String?): Result<Session> = resultOf {
        client.unshareSession(sessionId, directory).also { store.dispatch(AppEvent.SessionUpdated(it)) }
    }

    override suspend fun summarize(sessionId: String, model: ModelRef, directory: String?): Result<Unit> = resultOf {
        client.summarizeSession(sessionId, model, directory)
        Unit
    }

    override suspend fun abort(sessionId: String, directory: String?): Result<Unit> = resultOf {
        client.abortSession(sessionId, directory)
        Unit
    }

    override suspend fun replyPermission(requestId: String, reply: String, message: String?): Result<Unit> = resultOf {
        client.replyPermission(requestId, reply, message)
        store.dispatch(AppEvent.PermissionReplied(requestId))
    }

    override suspend fun replyQuestion(requestId: String, answers: List<List<String>>): Result<Unit> = resultOf {
        client.replyQuestion(requestId, answers)
        store.dispatch(AppEvent.QuestionReplied(requestId))
    }

    override suspend fun rejectQuestion(requestId: String): Result<Unit> = resultOf {
        client.rejectQuestion(requestId)
        store.dispatch(AppEvent.QuestionRejected(requestId))
    }

    override suspend fun fetchDaemonVersion(): String? =
        runCatching { client.getHealth() }.getOrNull()
}
