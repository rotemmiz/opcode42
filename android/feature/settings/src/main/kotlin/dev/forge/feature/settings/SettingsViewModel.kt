package dev.forge.feature.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.forge.feature.connections.ServerConnection
import dev.forge.feature.connections.ServerConnectionManager
import dev.forge.feature.notifications.PushController
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
import javax.inject.Inject

data class SettingsUiState(
    val connections: List<ServerConnection> = emptyList(),
    val activeKey: String? = null,
    val darkTheme: Boolean = true,
)

@HiltViewModel
class SettingsViewModel @Inject constructor(
    private val connectionManager: ServerConnectionManager,
    private val prefs: AppPreferences,
    private val pushController: PushController,
) : ViewModel() {

    val uiState: StateFlow<SettingsUiState> = combine(
        connectionManager.connections,
        connectionManager.activeServerConnectionFlow,
        prefs.darkTheme,
    ) { connections, active, darkTheme ->
        SettingsUiState(
            connections = connections,
            activeKey = active?.key(),
            darkTheme = darkTheme,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SettingsUiState())

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

    fun setDarkTheme(enabled: Boolean) = viewModelScope.launch { prefs.setDarkTheme(enabled) }
}
