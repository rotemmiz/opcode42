package dev.opcode42.core.network

import android.util.Log
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import dev.opcode42.core.model.*
import dev.opcode42.core.store.AppStore
import kotlinx.coroutines.*
import kotlinx.coroutines.channels.Channel
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.sse.EventSource
import okhttp3.sse.EventSourceListener
import okhttp3.sse.EventSources
import java.util.concurrent.atomic.AtomicBoolean
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.math.min

private const val TAG = "SseManager"

// Mirrors server-sdk.tsx constants exactly
private const val HEARTBEAT_TIMEOUT_MS = 15_000L
private const val FLUSH_FRAME_MS = 16L
private const val RECONNECT_DELAY_BASE_MS = 1_000L
private const val RECONNECT_DELAY_MAX_MS = 30_000L

/**
 * Manages the SSE connection to GET /global/event.
 *
 * Connection loop:
 *  - Foreground: maintain live OkHttp SSE connection.
 *  - Background: close connection (OS may suspend network I/O).
 *  - Return to foreground: immediately reconnect + reconcile state.
 *
 * Implements ProcessLifecycleOwner observer so it reacts to app foreground/background.
 */
@Singleton
class SseManager @Inject constructor(
    private val client: OkHttpClient,
    private val connectionProvider: ActiveConnectionProvider,
    private val store: AppStore,
    private val eventParser: SseEventParser,
) {
    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())
    private var connectionJob: Job? = null
    private val running = AtomicBoolean(false)
    private var lastEventAt = 0L

    // Batch buffer — events are coalesced here before dispatch
    private val batchChannel = Channel<SseEvent>(Channel.UNLIMITED)

    fun registerLifecycleObserver() {
        ProcessLifecycleOwner.get().lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onStart(owner: LifecycleOwner) {
                // Cold start (and every foreground after onStop cancelled the loops) lands here
                // with running == false → start() launches BOTH the connection loop and the
                // flush loop. Only force a reconnect when we're already running but have gone
                // stale (no event within the heartbeat window). Previously this routed cold
                // starts through reconnect() — which never started the flush loop — so events
                // piled up in batchChannel undrained and the UI never live-updated.
                if (!running.get()) {
                    start()
                } else if (System.currentTimeMillis() - lastEventAt >= HEARTBEAT_TIMEOUT_MS) {
                    reconnect()
                }
            }
            override fun onStop(owner: LifecycleOwner) = stop()
        })
    }

    private var directoryJob: Job? = null
    private var flushJob: Job? = null
    private var subscribedDirectory: String? = null

    fun start() {
        if (running.compareAndSet(false, true)) {
            connectionJob = scope.launch { connectionLoop("/global/event") }
        }
        // Always ensure the single consumer of batchChannel is alive — without it,
        // connectionLoop produces events that are never parsed/dispatched to the store.
        ensureFlushLoop()
    }

    /**
     * Launch the batch-flush loop if it isn't already running. [flushLoop] is the ONLY consumer
     * of [batchChannel] and the only place SSE events are parsed and dispatched to the store, so
     * every connection entry point (start/reconnect) must guarantee it is running.
     */
    private fun ensureFlushLoop() {
        if (flushJob?.isActive != true) {
            flushJob = scope.launch { flushLoop() }
        }
    }

    fun stop() {
        if (running.compareAndSet(true, false)) {
            connectionJob?.cancel()
            directoryJob?.cancel()
            flushJob?.cancel()
            connectionJob = null
            directoryJob = null
            flushJob = null
        }
    }

    fun reconnect() {
        connectionJob?.cancel()
        running.set(true)
        ensureFlushLoop()
        connectionJob = scope.launch { connectionLoop("/global/event") }
        subscribedDirectory?.let { subscribeDirectory(it) }
    }

    /** Subscribe to per-directory SSE stream for streaming message parts (A6). */
    fun subscribeDirectory(directory: String) {
        if (subscribedDirectory == directory && directoryJob?.isActive == true) return
        subscribedDirectory = directory
        directoryJob?.cancel()
        val encoded = java.net.URLEncoder.encode(directory, "UTF-8")
        directoryJob = scope.launch {
            var attempt = 0
            while (running.get()) {
                val baseUrl = connectionProvider.active?.url
                if (baseUrl == null) { delay(2_000); continue }
                connectOnce(baseUrl, "/event?directory=$encoded")
                if (!running.get()) break
                val backoff = min(RECONNECT_DELAY_BASE_MS * (1L shl attempt), RECONNECT_DELAY_MAX_MS)
                attempt = (attempt + 1).coerceAtMost(5)
                delay(backoff)
            }
        }
    }

    // ─── Connection loop ───────────────────────────────────────────────────────

    private suspend fun connectionLoop(path: String) {
        var attempt = 0
        while (running.get()) {
            val baseUrl = connectionProvider.active?.url
            if (baseUrl == null) {
                delay(2_000)
                continue
            }
            store.dispatch(AppEvent.Unknown(SseEvent(type = "connecting")))
            val connected = connectOnce(baseUrl, path)
            if (!running.get()) break
            val backoff = min(RECONNECT_DELAY_BASE_MS * (1L shl attempt), RECONNECT_DELAY_MAX_MS)
            Log.d(TAG, "SSE disconnected, retry in ${backoff}ms (attempt $attempt)")
            attempt = if (connected) 0 else (attempt + 1)
            delay(backoff)
        }
    }

    private suspend fun connectOnce(baseUrl: String, path: String): Boolean {
        val url = "$baseUrl$path"
        val request = Request.Builder().url(url).build()

        return suspendCancellableCoroutine { cont ->
            var eventSource: EventSource? = null
            var repeatHeartbeatJob: Job? = null

            val initialHeartbeatJob = scope.launch {
                delay(HEARTBEAT_TIMEOUT_MS)
                Log.w(TAG, "SSE heartbeat timeout — forcing reconnect")
                eventSource?.cancel()
                if (cont.isActive) cont.resume(false) {}
            }

            val listener = object : EventSourceListener() {
                override fun onOpen(es: EventSource, response: Response) {
                    lastEventAt = System.currentTimeMillis()
                    initialHeartbeatJob.cancel()
                    repeatHeartbeatJob = scope.launch {
                        store.dispatch(AppEvent.ServerConnected)
                        while (true) {
                            delay(HEARTBEAT_TIMEOUT_MS)
                            if (System.currentTimeMillis() - lastEventAt >= HEARTBEAT_TIMEOUT_MS) {
                                Log.w(TAG, "SSE heartbeat expired — reconnecting")
                                es.cancel()
                                break
                            }
                        }
                    }
                }

                override fun onEvent(es: EventSource, id: String?, type: String?, data: String) {
                    lastEventAt = System.currentTimeMillis()
                    // The SSE `event:` line is always "message"; the real event type
                    // lives inside the JSON `data` payload as `{id,type,properties}`.
                    // The /global/event stream additionally wraps that payload in a
                    // {payload:{...}, directory} envelope (Opcode42 internal/server/sse.go;
                    // opencode bus/global.ts). parseSseData unwraps both shapes.
                    parseSseData(data, fallbackId = id)?.let { batchChannel.trySend(it) }
                }

                override fun onClosed(es: EventSource) {
                    initialHeartbeatJob.cancel()
                    repeatHeartbeatJob?.cancel()
                    if (cont.isActive) cont.resume(true) {}
                }

                override fun onFailure(es: EventSource, t: Throwable?, response: Response?) {
                    initialHeartbeatJob.cancel()
                    repeatHeartbeatJob?.cancel()
                    Log.w(TAG, "SSE failure: ${t?.message}")
                    if (cont.isActive) cont.resume(false) {}
                }
            }

            eventSource = EventSources.createFactory(client).newEventSource(request, listener)

            cont.invokeOnCancellation {
                initialHeartbeatJob.cancel()
                repeatHeartbeatJob?.cancel()
                eventSource?.cancel()
            }
        }
    }

    // ─── Batch flush loop ──────────────────────────────────────────────────────

    /**
     * Collects events for up to FLUSH_FRAME_MS, then coalesces and dispatches.
     * Mirrors server-sdk.tsx flush() — drop stale deltas superseded by a full part update.
     */
    private suspend fun flushLoop() {
        while (true) {
            // Wait for at least one event
            val first = batchChannel.receive()
            val batch = mutableListOf(first)
            // Drain for one frame
            delay(FLUSH_FRAME_MS)
            while (true) {
                val next = batchChannel.tryReceive().getOrNull() ?: break
                batch.add(next)
            }
            // Coalesce: keep latest per (type, compound key)
            val coalesced = coalesce(batch)
            for (raw in coalesced) {
                // Guard the sole batchChannel consumer: a throwing reducer must not kill the
                // flush loop (it would silently stop all live updates — the very bug this fixes).
                try {
                    store.dispatch(eventParser.parse(raw))
                } catch (e: Exception) {
                    Log.w(TAG, "dropped SSE event; dispatch failed", e)
                }
            }
        }
    }

    /**
     * For coalesced types keep only the latest per key:
     *  - session.status → keyed by sessionID
     *  - message.part.updated → keyed by partID
     *  - lsp.updated → keyed by uri
     * Also suppress message.part.delta whose part already got a message.part.updated in this batch.
     */
    private fun coalesce(batch: List<SseEvent>): List<SseEvent> {
        val latestByKey = linkedMapOf<String, SseEvent>()
        val updatedPartIds = mutableSetOf<String>()

        for (event in batch) {
            when (event.type) {
                "session.status" -> {
                    val key = "session.status:" + (event.properties["sessionID"]?.jsonPrimitive?.content ?: "")
                    latestByKey[key] = event
                }
                "lsp.updated" -> {
                    val key = "lsp.updated:" + (event.properties["uri"]?.jsonPrimitive?.content ?: "")
                    latestByKey[key] = event
                }
                "message.part.updated" -> {
                    // properties = {sessionID, part:{id,...}, time} — part ID is nested.
                    val partId = (event.properties["part"] as? JsonObject)
                        ?.get("id")?.jsonPrimitive?.content ?: ""
                    val key = "message.part.updated:$partId"
                    latestByKey[key] = event
                    updatedPartIds.add(partId)
                }
                "message.part.delta" -> {
                    // properties = {sessionID, messageID, partID, field, delta}.
                    val partId = event.properties["partID"]?.jsonPrimitive?.content ?: ""
                    if (partId !in updatedPartIds) {
                        latestByKey["delta:$partId:${batch.indexOf(event)}"] = event
                    }
                }
                else -> latestByKey["${event.type}:${batch.indexOf(event)}"] = event
            }
        }
        return latestByKey.values.toList()
    }
}
