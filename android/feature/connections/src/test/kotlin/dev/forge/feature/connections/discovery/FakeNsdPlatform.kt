package dev.forge.feature.connections.discovery

/**
 * Test double for [NsdPlatform]. Lets a test drive discovery synchronously: emit found/lost
 * services, then complete each queued resolve in order — exercising [DiscoveryManager]'s serial
 * resolve queue, de-dupe, and lifecycle without Android.
 */
class FakeNsdPlatform : NsdPlatform {
    var started = false
        private set
    var startCount = 0
        private set
    var stopCount = 0
        private set
    var lastServiceType: String? = null
        private set

    private var callbacks: NsdPlatform.Callbacks? = null

    /** Resolves awaiting completion via [completeNextResolve], FIFO. */
    private val resolveQueue = ArrayDeque<Pair<RawService, (DiscoveredServer?) -> Unit>>()

    val pendingResolveCount: Int get() = resolveQueue.size

    override fun start(serviceType: String, callbacks: NsdPlatform.Callbacks) {
        started = true
        startCount++
        lastServiceType = serviceType
        this.callbacks = callbacks
    }

    override fun stop() {
        started = false
        stopCount++
        callbacks = null
        resolveQueue.clear()
    }

    override fun resolve(service: RawService, onResult: (DiscoveredServer?) -> Unit) {
        resolveQueue.addLast(service to onResult)
    }

    // ─── Test drivers ──────────────────────────────────────────────────────────

    fun emitFound(name: String, type: String = DiscoveryManager.DEFAULT_SERVICE_TYPE) {
        callbacks?.onServiceFound(RawService(name, type))
    }

    fun emitLost(name: String, type: String = DiscoveryManager.DEFAULT_SERVICE_TYPE) {
        callbacks?.onServiceLost(RawService(name, type))
    }

    /** Complete the oldest pending resolve with [result] (null = resolve failed). */
    fun completeNextResolve(result: DiscoveredServer?) {
        val (_, onResult) = resolveQueue.removeFirst()
        onResult(result)
    }

    fun nextResolveName(): String = resolveQueue.first().first.name
}
