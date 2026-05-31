package dev.forge.feature.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.forge.feature.connections.ServerConnection
import dev.forge.feature.connections.ServerConnectionManager
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
) : ViewModel() {

    val uiState: StateFlow<SettingsUiState> = combine(
        connectionManager.connections,
        connectionManager.activeFlow,
        prefs.darkTheme,
    ) { connections, active, darkTheme ->
        SettingsUiState(
            connections = connections,
            activeKey = active?.key(),
            darkTheme = darkTheme,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SettingsUiState())

    fun removeServer(key: String) = connectionManager.remove(key)
    fun setActiveServer(key: String) = connectionManager.setActive(key)
    fun setDarkTheme(enabled: Boolean) = viewModelScope.launch { prefs.setDarkTheme(enabled) }
}
