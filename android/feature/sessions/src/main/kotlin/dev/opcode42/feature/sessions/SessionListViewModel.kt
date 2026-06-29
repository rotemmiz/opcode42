package dev.opcode42.feature.sessions

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.opcode42.core.model.AppEvent
import dev.opcode42.core.model.PermissionRequest
import dev.opcode42.core.model.QuestionRequest
import dev.opcode42.core.model.Session
import dev.opcode42.core.network.ActiveConnectionProvider
import dev.opcode42.core.sdk.Opcode42Client
import dev.opcode42.core.store.AppState
import dev.opcode42.core.store.AppStore
import dev.opcode42.feature.sessions.ui.isSessionBusy
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

/** True when the session has been archived (opencode sets `time.archived` to a number). */
val Session.isArchived: Boolean
    get() = time?.archived != null

/** Status-filter tab (Claude Code style): everything, in-flight, or needs-the-user. */
enum class SessionFilter { All, Working, NeedsInput }

/** A date-bucketed run of sessions (e.g. header "Today" + its rows), recency-ordered. */
data class SessionGroup(val header: String, val sessions: List<Session>)

data class SessionListUiState(
    /** Sessions to display, filtered + searched + grouped under date headers. */
    val groups: List<SessionGroup> = emptyList(),
    /** Tab counts over the active (non-archived, top-level) set — independent of the active tab. */
    val allCount: Int = 0,
    val workingCount: Int = 0,
    val needsInputCount: Int = 0,
    /** Active status-filter tab. */
    val filter: SessionFilter = SessionFilter.All,
    /** Current search query (title/directory substring). */
    val query: String = "",
    /** Count of archived sessions, for the "Archived (n)" affordance. */
    val archivedCount: Int = 0,
    /** When true the list shows archived sessions instead of active ones. */
    val showArchived: Boolean = false,
    /** sessionID → status string ("busy" while a turn is in flight, else "idle"). */
    val statuses: Map<String, String> = emptyMap(),
    /** sessionID → first pending permission request, for inline Approve/Deny in the menu. */
    val pendingPermissions: Map<String, PermissionRequest> = emptyMap(),
    /** sessionID → first pending question, for an inline reply field in the menu. */
    val pendingQuestions: Map<String, QuestionRequest> = emptyMap(),
    val isLoading: Boolean = false,
    val error: String? = null,
)

/** The slice of [AppState] the session list reads — gates re-projection and is the projection input. */
internal data class SessionInputs(
    val sessions: List<Session>,
    val sessionStatus: Map<String, String>,
    val permissions: Map<String, List<PermissionRequest>>,
    val questions: Map<String, List<QuestionRequest>>,
)

internal fun AppState.toSessionInputs() = SessionInputs(sessions, sessionStatus, permissions, questions)

/**
 * Pure projection from the store slice to the list UI state. Kept side-effect-free and
 * top-level so the child-hiding, tab counts, search/filter, recency ordering, and date
 * grouping can be unit-tested without a ViewModel or coroutines. Takes [SessionInputs]
 * (not the whole [AppState]) so its data dependencies are explicit and match the
 * `distinctUntilChanged` gate exactly. `now` is injectable so date buckets are deterministic.
 */
internal fun projectSessionList(
    inputs: SessionInputs,
    showArchived: Boolean,
    query: String,
    filter: SessionFilter,
    now: Long = System.currentTimeMillis(),
): SessionListUiState {
    // Hide sub-agent (`task`) child sessions — they carry a parentID and are an
    // implementation detail of the parent turn, not user-initiated sessions.
    val topLevel = inputs.sessions.filter { it.parentID == null }
    val (archived, active) = topLevel.partition { it.isArchived }

    val statuses = inputs.sessionStatus
    // First pending request per session — the menu shows one actionable affordance per row.
    val pendingPermissions = inputs.permissions
        .mapNotNull { (id, list) -> list.firstOrNull()?.let { id to it } }
        .toMap()
    val pendingQuestions = inputs.questions
        .mapNotNull { (id, list) -> list.firstOrNull()?.let { id to it } }
        .toMap()

    fun working(s: Session) = isSessionBusy(statuses[s.id])
    fun needsInput(s: Session) = pendingPermissions.containsKey(s.id) || pendingQuestions.containsKey(s.id)

    // Tab counts always reflect the full active set, so the badges don't change with the tab.
    val allCount = active.size
    val workingCount = active.count { working(it) }
    val needsInputCount = active.count { needsInput(it) }

    val base = if (showArchived) archived else active
    // Filter tabs apply to the active list only; archived is its own mode.
    val afterFilter = if (showArchived) base else when (filter) {
        SessionFilter.All -> base
        SessionFilter.Working -> base.filter { working(it) }
        SessionFilter.NeedsInput -> base.filter { needsInput(it) }
    }
    val q = query.trim()
    val afterSearch = if (q.isEmpty()) afterFilter else afterFilter.filter { s ->
        s.title?.contains(q, ignoreCase = true) == true ||
            s.directory?.contains(q, ignoreCase = true) == true
    }
    // Most-recently-active first; group by date bucket. groupBy preserves encounter order,
    // so the descending sort makes "Today" the first group, then "Yesterday", etc.
    val groups = afterSearch
        .sortedByDescending { it.time?.updated ?: it.time?.created ?: 0L }
        .groupBy { dateBucket(it.time?.updated ?: it.time?.created ?: 0L, now) }
        .map { (header, sessions) -> SessionGroup(header, sessions) }

    return SessionListUiState(
        groups = groups,
        allCount = allCount,
        workingCount = workingCount,
        needsInputCount = needsInputCount,
        filter = filter,
        query = query,
        archivedCount = archived.size,
        showArchived = showArchived,
        statuses = statuses,
        pendingPermissions = pendingPermissions,
        pendingQuestions = pendingQuestions,
    )
}

@HiltViewModel
class SessionListViewModel @Inject constructor(
    private val client: Opcode42Client,
    private val store: AppStore,
    private val connectionProvider: ActiveConnectionProvider,
) : ViewModel() {

    // Local view toggle: active list vs. archived list. opencode's session list returns
    // both; filtering is client-side (the daemon does not drop archived sessions).
    private val _showArchived = MutableStateFlow(false)
    private val _query = MutableStateFlow("")
    private val _filter = MutableStateFlow(SessionFilter.All)

    val uiState: StateFlow<SessionListUiState> =
        combine(
            // Only re-project when a field the list actually reads changes — not on every
            // message/part streaming delta, which would otherwise re-sort + re-group + do
            // per-session LocalDate math over the whole global list on the main thread.
            store.state.map { it.toSessionInputs() }.distinctUntilChanged(),
            _showArchived,
            _query,
            _filter,
        ) { inputs, showArchived, query, filter ->
            projectSessionList(inputs, showArchived, query, filter)
        }.flowOn(Dispatchers.Default)
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SessionListUiState())

    private val _isCreating = MutableStateFlow(false)
    val isCreating: StateFlow<Boolean> = _isCreating.asStateFlow()

    init {
        loadSessions()
        // Re-fetch when a connection becomes active (e.g., first-time server add)
        viewModelScope.launch {
            connectionProvider.activeFlow
                .filterNotNull()
                .distinctUntilChanged()
                .collect { loadSessions() }
        }
    }

    fun toggleShowArchived() {
        _showArchived.value = !_showArchived.value
    }

    fun setQuery(query: String) {
        _query.value = query
    }

    fun setFilter(filter: SessionFilter) {
        _filter.value = filter
    }

    /**
     * Aggregate sessions across **every project/directory** the daemon knows, so the list is
     * a global view that needs no configured working folder. Enumerate `GET /project`, fan out
     * `listSessions(dir)` per worktree + sandbox in parallel, plus one no-directory call (covers
     * the daemon's default/CWD project and the Opcode42 "all sessions" case), then dedupe by id.
     * Every call is wrapped so one unreachable directory never blanks the whole list.
     */
    fun loadSessions() {
        viewModelScope.launch {
            try {
                val projects = runCatching { client.listProjects() }.getOrDefault(emptyList())
                val dirs = projects
                    .flatMap { listOf(it.worktree) + it.sandboxes }
                    .filterNotNull()
                    .filter { it != "/" } // root worktree of the synthetic "global" project
                    .toSet()
                val sessions = coroutineScope {
                    val perDir = dirs.map { dir ->
                        async { runCatching { client.listSessions(dir) }.getOrDefault(emptyList()) }
                    }
                    val global = async { runCatching { client.listSessions(null) }.getOrDefault(emptyList()) }
                    perDir.awaitAll().flatten() + global.await()
                }
                sessions.distinctBy { it.id }.forEach { session ->
                    store.dispatch(AppEvent.SessionUpdated(session))
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                // Sessions will load from SSE events once connected
            }
        }
    }

    fun forkSession(sessionId: String, onForked: (Session) -> Unit) {
        viewModelScope.launch {
            try {
                val newSession = client.forkSession(sessionId)
                store.dispatch(AppEvent.SessionUpdated(newSession))
                onForked(newSession)
            } catch (e: Exception) {
                android.util.Log.e("SessionListVM", "forkSession failed", e)
            }
        }
    }

    /** PATCH /session/{id} title; the returned session updates the store (and SSE echoes it). */
    fun renameSession(sessionId: String, title: String) {
        val trimmed = title.trim()
        if (trimmed.isEmpty()) return
        viewModelScope.launch {
            try {
                store.dispatch(AppEvent.SessionUpdated(client.renameSession(sessionId, trimmed)))
            } catch (e: Exception) {
                android.util.Log.e("SessionListVM", "renameSession failed", e)
            }
        }
    }

    /**
     * Archive the session via PATCH /session/{id} `time.archived`. The returned session
     * (now archived) updates the store, so it drops out of the active list. There is no
     * un-archive path — opencode treats archived as set-only.
     */
    fun archiveSession(sessionId: String) {
        viewModelScope.launch {
            try {
                store.dispatch(AppEvent.SessionUpdated(client.archiveSession(sessionId)))
            } catch (e: Exception) {
                android.util.Log.e("SessionListVM", "archiveSession failed", e)
            }
        }
    }

    fun deleteSession(sessionId: String) {
        viewModelScope.launch {
            try {
                client.deleteSession(sessionId)
                store.dispatch(AppEvent.SessionRemoved(sessionId))
            } catch (e: Exception) {
                android.util.Log.e("SessionListVM", "deleteSession failed", e)
            }
        }
    }

    // ── In-menu pending-action replies ────────────────────────────────────────
    // Mirrors ChatViewModel.replyPermission/replyQuestion/rejectQuestion so a session
    // that needs the user can be answered straight from the sessions menu, without
    // opening it. The store maps drop the request on the *Replied/*Rejected event.

    /** Approve or deny a session's pending permission from the menu. */
    fun replyPermission(requestId: String, allow: Boolean) {
        viewModelScope.launch {
            try {
                client.replyPermission(requestId, allow)
                store.dispatch(AppEvent.PermissionReplied(requestId))
            } catch (_: Exception) { }
        }
    }

    /** Answer a session's pending question from the menu. */
    fun replyQuestion(requestId: String, answer: String) {
        viewModelScope.launch {
            try {
                client.replyQuestion(requestId, answer)
                store.dispatch(AppEvent.QuestionReplied(requestId))
            } catch (_: Exception) { }
        }
    }

    /** Skip a session's pending question from the menu. */
    fun rejectQuestion(requestId: String) {
        viewModelScope.launch {
            try {
                client.rejectQuestion(requestId)
                store.dispatch(AppEvent.QuestionRejected(requestId))
            } catch (_: Exception) { }
        }
    }

    fun createSession(directory: String? = null, onCreated: (Session) -> Unit) {
        viewModelScope.launch {
            _isCreating.value = true
            try {
                val session = client.createSession(directory)
                store.dispatch(AppEvent.SessionUpdated(session))
                onCreated(session)
            } catch (_: Exception) {
            } finally {
                _isCreating.value = false
            }
        }
    }
}
