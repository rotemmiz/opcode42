package dev.forge.feature.sessions

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.forge.core.model.Session
import dev.forge.core.sdk.ForgeClient
import dev.forge.core.store.AppStore
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

data class SessionListUiState(
    val sessions: List<Session> = emptyList(),
    val isLoading: Boolean = false,
    val error: String? = null,
)

@HiltViewModel
class SessionListViewModel @Inject constructor(
    private val client: ForgeClient,
    private val store: AppStore,
) : ViewModel() {

    val uiState: StateFlow<SessionListUiState> = store.state
        .map { appState ->
            SessionListUiState(sessions = appState.sessions)
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SessionListUiState())

    private val _isCreating = MutableStateFlow(false)
    val isCreating: StateFlow<Boolean> = _isCreating.asStateFlow()

    init {
        loadSessions()
    }

    fun loadSessions() {
        viewModelScope.launch {
            try {
                val sessions = client.listSessions()
                // Seed the store so the StateFlow reflects fetched data
                sessions.forEach { session ->
                    store.dispatch(dev.forge.core.model.AppEvent.SessionUpdated(session))
                }
            } catch (e: Exception) {
                // Sessions will load from SSE events once connected
            }
        }
    }

    fun createSession(directory: String? = null, onCreated: (Session) -> Unit) {
        viewModelScope.launch {
            _isCreating.value = true
            try {
                val session = client.createSession(directory)
                store.dispatch(dev.forge.core.model.AppEvent.SessionUpdated(session))
                onCreated(session)
            } catch (_: Exception) {
            } finally {
                _isCreating.value = false
            }
        }
    }
}
