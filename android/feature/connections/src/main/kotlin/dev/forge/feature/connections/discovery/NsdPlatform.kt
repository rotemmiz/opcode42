package dev.forge.feature.connections.discovery

/** A service seen while browsing, before it has been resolved to host/port/TXT. */
data class RawService(val name: String, val type: String)

/**
 * Platform mDNS operations, abstracted behind an interface so [DiscoveryManager] carries no Android
 * dependency and is unit-testable against a fake. The real implementation
 * ([AndroidNsdPlatform]) wraps `NsdManager` + `WifiManager`.
 *
 * Implementations MUST deliver all callbacks on a single, consistent thread (the main thread for
 * [AndroidNsdPlatform]) so the manager's internal state needs no locking.
 */
interface NsdPlatform {
    /** Acquire the multicast lock and begin browsing for [serviceType]. */
    fun start(serviceType: String, callbacks: Callbacks)

    /** Stop browsing and release the multicast lock. Safe to call when not started. */
    fun stop()

    /**
     * Resolve a found service to host/port/TXT. Invokes [onResult] exactly once — with the
     * resolved server, or `null` on failure. Resolves are driven serially by the manager.
     */
    fun resolve(service: RawService, onResult: (DiscoveredServer?) -> Unit)

    interface Callbacks {
        fun onServiceFound(service: RawService)
        fun onServiceLost(service: RawService)
    }
}
