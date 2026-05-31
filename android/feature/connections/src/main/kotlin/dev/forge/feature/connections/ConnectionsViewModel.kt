package dev.forge.feature.connections

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.forge.feature.connections.discovery.AuthType
import dev.forge.feature.connections.discovery.DiscoveredServer
import dev.forge.feature.connections.discovery.DiscoveryManager
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import javax.inject.Inject

/** A discovered daemon paired with whether it already exists in the saved connections. */
data class DiscoveredEntry(
    val server: DiscoveredServer,
    val alreadyAdded: Boolean,
)

@HiltViewModel
class ConnectionsViewModel @Inject constructor(
    private val manager: ServerConnectionManager,
    private val discovery: DiscoveryManager,
) : ViewModel() {
    val connections: StateFlow<List<ServerConnection>> = manager.connections
    val active: StateFlow<ServerConnection?> = manager.activeFlow

    val scanning: StateFlow<Boolean> = discovery.scanning

    /** Servers found on the LAN, each flagged if it matches an already-saved connection. */
    val discovered: StateFlow<List<DiscoveredEntry>> =
        combine(discovery.servers, manager.connections) { found, saved ->
            val savedUrls = saved.map { it.http.url }.toSet()
            found.map { server ->
                DiscoveredEntry(server, alreadyAdded = normalizeServerUrl(server.url) in savedUrls)
            }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    fun startDiscovery() = discovery.start()

    fun stopDiscovery() = discovery.stop()

    /** True when the daemon advertised that it needs credentials (`auth=basic|token`). */
    fun requiresCredentials(server: DiscoveredServer): Boolean =
        server.authType == AuthType.BASIC || server.authType == AuthType.TOKEN

    /** Add a discovered daemon directly — for servers that need no credentials. */
    fun addDiscovered(server: DiscoveredServer) =
        manager.add(server.url, displayName = server.serviceName)

    fun addServer(rawUrl: String, username: String? = null, password: String? = null, displayName: String? = null) =
        manager.add(rawUrl, username, password, displayName)

    fun removeServer(key: String) = manager.remove(key)

    fun setActive(key: String) = manager.setActive(key)
}
