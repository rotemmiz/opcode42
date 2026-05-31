package dev.forge.feature.connections.discovery

import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.net.wifi.WifiManager
import android.os.Handler
import android.os.Looper

/**
 * Real [NsdPlatform] over Android's [NsdManager] + [WifiManager].
 *
 * All [NsdPlatform.Callbacks] and resolve results are posted to the main thread so
 * [DiscoveryManager] stays single-threaded (NsdManager's listeners otherwise arrive on a mix of
 * the main looper and binder threads).
 *
 * Uses the (API-34-deprecated) `resolveService`; the manager serializes resolves to dodge the
 * pre-34 "listener already in use" crash. A `registerServiceInfoCallback` path can replace this on
 * 34+ later (plan 07).
 */
class AndroidNsdPlatform(
    private val nsd: NsdManager,
    private val wifi: WifiManager,
) : NsdPlatform {

    private val main = Handler(Looper.getMainLooper())

    private var multicastLock: WifiManager.MulticastLock? = null
    private var discoveryListener: NsdManager.DiscoveryListener? = null

    /** Full [NsdServiceInfo] for each found service, keyed by name — needed to resolve it. */
    private val found = mutableMapOf<String, NsdServiceInfo>()

    override fun start(serviceType: String, callbacks: NsdPlatform.Callbacks) {
        multicastLock = wifi.createMulticastLock(MULTICAST_LOCK_TAG).apply {
            setReferenceCounted(true)
            runCatching { acquire() }
        }

        val listener = object : NsdManager.DiscoveryListener {
            override fun onStartDiscoveryFailed(serviceType: String?, errorCode: Int) {
                runCatching { nsd.stopServiceDiscovery(this) }
            }

            override fun onStopDiscoveryFailed(serviceType: String?, errorCode: Int) {}
            override fun onDiscoveryStarted(serviceType: String?) {}
            override fun onDiscoveryStopped(serviceType: String?) {}

            override fun onServiceFound(info: NsdServiceInfo) {
                found[info.serviceName] = info
                main.post { callbacks.onServiceFound(RawService(info.serviceName, info.serviceType)) }
            }

            override fun onServiceLost(info: NsdServiceInfo) {
                found.remove(info.serviceName)
                main.post { callbacks.onServiceLost(RawService(info.serviceName, info.serviceType)) }
            }
        }
        discoveryListener = listener
        runCatching { nsd.discoverServices(serviceType, NsdManager.PROTOCOL_DNS_SD, listener) }
    }

    override fun stop() {
        discoveryListener?.let { listener -> runCatching { nsd.stopServiceDiscovery(listener) } }
        discoveryListener = null
        found.clear()
        multicastLock?.let { lock -> runCatching { if (lock.isHeld) lock.release() } }
        multicastLock = null
    }

    @Suppress("DEPRECATION") // resolveService deprecated on API 34; serial-resolve path (plan 07).
    override fun resolve(service: RawService, onResult: (DiscoveredServer?) -> Unit) {
        val info = found[service.name] ?: run { onResult(null); return }
        val listener = object : NsdManager.ResolveListener {
            override fun onResolveFailed(serviceInfo: NsdServiceInfo?, errorCode: Int) {
                main.post { onResult(null) }
            }

            override fun onServiceResolved(resolved: NsdServiceInfo) {
                val host = resolved.host?.let { it.hostAddress ?: it.hostName }
                val server = host?.let {
                    DiscoveredServer(
                        serviceName = resolved.serviceName,
                        host = it,
                        port = resolved.port,
                        txt = parseTxtAttributes(resolved.attributes ?: emptyMap()),
                    )
                }
                main.post { onResult(server) }
            }
        }
        runCatching { nsd.resolveService(info, listener) }
            .onFailure { main.post { onResult(null) } }
    }

    private companion object {
        const val MULTICAST_LOCK_TAG = "forge-mdns"
    }
}
