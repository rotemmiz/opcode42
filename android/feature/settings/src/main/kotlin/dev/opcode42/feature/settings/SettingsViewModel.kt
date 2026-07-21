package dev.opcode42.feature.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.opcode42.core.data.SessionRepository
import dev.opcode42.core.store.ConnectionState
import dev.opcode42.feature.connections.ServerConnection
import dev.opcode42.feature.connections.ServerConnectionManager
import dev.opcode42.feature.notifications.PushController
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
import javax.inject.Inject

data class SettingsUiState(
    val connections: List<ServerConnection> = emptyList(),
    val activeKey: String? = null,
    val themeMode: ThemeMode = ThemeMode.System,
    /** Dynamic color (Material You) opt-in — drives the Material You color scheme on API 31+. */
    val dynamicColor: Boolean = false,
    /** SSE connection state for the active server — drives the server-row status dot (G1). */
    val activeConnectionState: ConnectionState = ConnectionState.Disconnected,
    /** Daemon version from `GET /global/health`, for the About section (G1). null = not fetched. */
    val daemonVersion: String? = null,
)

@HiltViewModel
class SettingsViewModel @Inject constructor(
    private val connectionManager: ServerConnectionManager,
    private val pushController: PushController,
    private val appPreferences: AppPreferences,
    private val sessionRepo: SessionRepository,
) : ViewModel() {

    private val _daemonVersion = MutableStateFlow<String?>(null)

    val uiState: StateFlow<SettingsUiState> = combine(
        connectionManager.connections,
        connectionManager.activeServerConnectionFlow,
        appPreferences.themeMode,
        appPreferences.dynamicColor,
        sessionRepo.connectionState,
    ) { connections, active, themeMode, dynamicColor, connState ->
        SettingsUiState(
            connections = connections,
            activeKey = active?.key(),
            themeMode = themeMode,
            dynamicColor = dynamicColor,
            activeConnectionState = connState,
        )
    }.combine(_daemonVersion) { state, ver ->
        state.copy(daemonVersion = ver)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SettingsUiState())

    init {
        // Fetch the daemon version once for the About section.
        viewModelScope.launch {
            runCatching { sessionRepo.fetchDaemonVersion() }
                .onSuccess { ver -> _daemonVersion.value = ver }
        }
    }

    fun setThemeMode(mode: ThemeMode) {
        viewModelScope.launch { appPreferences.setThemeMode(mode) }
    }

    fun setDynamicColor(enabled: Boolean) {
        viewModelScope.launch { appPreferences.setDynamicColor(enabled) }
    }

    /**
     * Removes a server. When the *active* server is removed we first unregister
     * this device's push token from its daemon (the DELETE must hit the daemon
     * we are leaving, so it runs before the active connection switches away),
     * then re-sync registration against whichever server becomes active.
     */
    fun removeServer(key: String) {
        val wasActive = connectionManager.activeServerConnectionFlow.value?.key() == key
        if (wasActive) {
            viewModelScope.launch {
                pushController.logoutAndAwait()
                connectionManager.remove(key)
                pushController.start()
            }
        } else {
            connectionManager.remove(key)
        }
    }

    fun setActiveServer(key: String) {
        connectionManager.setActive(key)
        // Register this device with the newly-active daemon.
        pushController.start()
    }
}
