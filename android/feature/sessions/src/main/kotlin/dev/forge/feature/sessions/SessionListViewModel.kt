package dev.forge.feature.sessions

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.forge.core.model.AppEvent
import dev.forge.core.model.Session
import dev.forge.core.sdk.ForgeClient
import dev.forge.core.store.AppStore
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

/** True when the session has been archived (opencode sets `time.archived` to a number). */
val Session.isArchived: Boolean
    get() = time?.archived != null

data class SessionListUiState(
    /** Sessions to display: active sessions, or archived ones when [showArchived] is set. */
    val sessions: List<Session> = emptyList(),
    /** Count of archived sessions, for the "Archived (n)" affordance. */
    val archivedCount: Int = 0,
    /** When true the list shows archived sessions instead of active ones. */
    val showArchived: Boolean = false,
    val isLoading: Boolean = false,
    val error: String? = null,
)

@HiltViewModel
class SessionListViewModel @Inject constructor(
    private val client: ForgeClient,
    private val store: AppStore,
) : ViewModel() {

    // Local view toggle: active list vs. archived list. opencode's session list returns
    // both; filtering is client-side (the daemon does not drop archived sessions).
    private val _showArchived = MutableStateFlow(false)

    val uiState: StateFlow<SessionListUiState> =
        combine(store.state, _showArchived) { appState, showArchived ->
            val (archived, active) = appState.sessions.partition { it.isArchived }
            SessionListUiState(
                sessions = if (showArchived) archived else active,
                archivedCount = archived.size,
                showArchived = showArchived,
            )
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SessionListUiState())

    private val _isCreating = MutableStateFlow(false)
    val isCreating: StateFlow<Boolean> = _isCreating.asStateFlow()

    init {
        loadSessions()
    }

    fun toggleShowArchived() {
        _showArchived.value = !_showArchived.value
    }

    fun loadSessions() {
        viewModelScope.launch {
            try {
                val sessions = client.listSessions()
                // Seed the store so the StateFlow reflects fetched data
                sessions.forEach { session ->
                    store.dispatch(AppEvent.SessionUpdated(session))
                }
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
