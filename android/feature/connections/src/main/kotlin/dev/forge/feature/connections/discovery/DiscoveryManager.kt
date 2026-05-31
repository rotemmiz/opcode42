package dev.forge.feature.connections.discovery

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Browses the LAN for opencode-compatible daemons and exposes the resolved set as a flow.
 *
 * Orchestration only — the Android plumbing lives in [NsdPlatform]. Responsibilities:
 *  - **Serial resolve.** `NsdManager.resolveService` can throw if called concurrently pre-API-34,
 *    so found services are queued and resolved one at a time (plan 07).
 *  - **De-dupe** by `host:port` so two TXT records or a re-announce don't double-list a daemon.
 *  - **Lifecycle.** [start]/[stop] acquire/release the multicast lock via the platform; [stop]
 *    clears the list and drops any in-flight resolve.
 *
 * Single-threaded: relies on [NsdPlatform] delivering every callback on one thread.
 */
@Singleton
class DiscoveryManager @Inject constructor(
    private val platform: NsdPlatform,
) {
    private val _servers = MutableStateFlow<List<DiscoveredServer>>(emptyList())
    val servers: StateFlow<List<DiscoveredServer>> = _servers.asStateFlow()

    private val _scanning = MutableStateFlow(false)
    val scanning: StateFlow<Boolean> = _scanning.asStateFlow()

    private var started = false

    // Serial-resolve state (touched only on the platform's callback thread).
    private val pending = ArrayDeque<RawService>()
    private var resolving = false

    private val callbacks = object : NsdPlatform.Callbacks {
        override fun onServiceFound(service: RawService) = enqueueResolve(service)
        override fun onServiceLost(service: RawService) = removeByName(service.name)
    }

    fun start(serviceType: String = DEFAULT_SERVICE_TYPE) {
        if (started) return
        started = true
        _scanning.value = true
        platform.start(serviceType, callbacks)
    }

    fun stop() {
        if (!started) return
        started = false
        _scanning.value = false
        pending.clear()
        resolving = false
        platform.stop()
        _servers.value = emptyList()
    }

    private fun enqueueResolve(service: RawService) {
        if (!started) return
        if (pending.any { it.name == service.name }) return
        pending.addLast(service)
        pumpResolve()
    }

    private fun pumpResolve() {
        if (resolving) return
        val next = pending.removeFirstOrNull() ?: return
        resolving = true
        platform.resolve(next) { resolved ->
            resolving = false
            if (started && resolved != null) addOrUpdate(resolved)
            if (started) pumpResolve()
        }
    }

    private fun addOrUpdate(server: DiscoveredServer) {
        val hostPort = "${server.host}:${server.port}"
        val others = _servers.value.filterNot { "${it.host}:${it.port}" == hostPort }
        _servers.value = (others + server).sortedBy { it.serviceName.lowercase() }
    }

    private fun removeByName(name: String) {
        _servers.value = _servers.value.filterNot { it.serviceName == name }
    }

    companion object {
        const val DEFAULT_SERVICE_TYPE = "_opencode._tcp."
    }
}
