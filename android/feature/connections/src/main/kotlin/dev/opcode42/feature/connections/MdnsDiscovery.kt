package dev.opcode42.feature.connections

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.net.wifi.WifiManager
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import javax.inject.Inject
import javax.inject.Singleton

/**
 * A discovered mDNS server — the resolved host:port + the service name (e.g. `opencode-4096`).
 */
data class DiscoveredServer(val name: String, val host: String, val port: Int) {
    val url: String get() = "http://$host:$port"
}

/**
 * LAN mDNS discovery for opencode/Opcode42 daemons. Browses two service types in parallel:
 *  - `_http._tcp` — what opencode advertises (`packages/opencode/src/server/mdns.ts:14-20`).
 *  - `_opencode._tcp` — Opcode42's brand service type (plan 13).
 *
 * Filters by service-name prefix `opencode-` / `opcode42-` so non-opencode HTTP services on the
 * LAN are not surfaced. Acquires a [WifiManager.MulticastLock] while discovering (multicast
 * packets are dropped on most Android devices without it). Lifecycle-scoped by [start]/[stop].
 */
@Singleton
class MdnsDiscovery @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    private val _servers = MutableStateFlow<List<DiscoveredServer>>(emptyList())
    val servers: StateFlow<List<DiscoveredServer>> = _servers.asStateFlow()

    private var nsdManager: NsdManager? = null
    private var multicastLock: WifiManager.MulticastLock? = null
    private val activeListeners = mutableMapOf<String, NsdManager.DiscoveryListener>()

    /**
     * Start browsing. Safe to call repeatedly; a no-op if already running.
     */
    fun start() {
        if (nsdManager != null) return
        val manager = context.getSystemService(Context.NSD_SERVICE) as? NsdManager ?: return
        nsdManager = manager

        // Multicast lock — without it multicast DNS packets are dropped on most devices.
        val wifi = context.getSystemService(Context.WIFI_SERVICE) as? WifiManager
        multicastLock = wifi?.createMulticastLock("opcode42-mdns")?.apply {
            setReferenceCounted(false)
            acquire()
        }

        listOf("_http._tcp", "_opencode._tcp").forEach { serviceType ->
            val listener = object : NsdManager.DiscoveryListener {
                override fun onDiscoveryStarted(serviceType: String) {}
                override fun onDiscoveryStopped(serviceType: String) {}

                override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) {
                    activeListeners.remove(serviceType)
                }

                override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) {}

                override fun onServiceFound(info: NsdServiceInfo) {
                    val name = info.serviceName
                    // Only opencode/opcode42 daemons — filter by name prefix.
                    if (!name.startsWith("opencode-") && !name.startsWith("opcode42-")) return
                    resolve(info)
                }

                override fun onServiceLost(info: NsdServiceInfo) {
                    _servers.value = _servers.value.filterNot { it.name == info.serviceName }
                }
            }
            activeListeners[serviceType] = listener
            runCatching { manager.discoverServices(serviceType, NsdManager.PROTOCOL_DNS_SD, listener) }
        }
    }

    private fun resolve(info: NsdServiceInfo) {
        val manager = nsdManager ?: return
        val resolveListener = object : NsdManager.ResolveListener {
            override fun onServiceResolved(serviceInfo: NsdServiceInfo) {
                val host = serviceInfo.host?.hostAddress ?: return
                val port = serviceInfo.port
                val server = DiscoveredServer(serviceInfo.serviceName, host, port)
                // Dedupe by (host, port) across the two browse types.
                _servers.value = (_servers.value + server).distinctBy { it.host to it.port }
            }

            override fun onResolveFailed(serviceInfo: NsdServiceInfo, errorCode: Int) {}
        }
        runCatching { manager.resolveService(info, resolveListener) }
    }

    /**
     * Stop browsing and release the multicast lock. Call on screen dispose.
     */
    fun stop() {
        activeListeners.forEach { (type, listener) ->
            runCatching { nsdManager?.stopServiceDiscovery(listener) }
        }
        activeListeners.clear()
        multicastLock?.let { if (it.isHeld) it.release() }
        multicastLock = null
        _servers.value = emptyList()
        nsdManager = null
    }
}