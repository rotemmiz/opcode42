package dev.forge.feature.connections

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.StateFlow
import javax.inject.Inject

@HiltViewModel
class ConnectionsViewModel @Inject constructor(
    private val manager: ServerConnectionManager,
) : ViewModel() {
    val connections: StateFlow<List<ServerConnection>> = manager.connections
    val active: StateFlow<ServerConnection?> = manager.activeFlow

    fun addServer(rawUrl: String, username: String? = null, password: String? = null, displayName: String? = null) =
        manager.add(rawUrl, username, password, displayName)

    fun removeServer(key: String) = manager.remove(key)

    fun setActive(key: String) = manager.setActive(key)
}
