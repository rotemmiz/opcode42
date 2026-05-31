package dev.forge.core.network

import android.util.Log
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import dev.forge.core.model.*
import dev.forge.core.store.AppStore
import dev.forge.core.store.ConnectionState
import kotlinx.coroutines.*
import kotlinx.coroutines.channels.Channel
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.jsonObject
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
                val stale = System.currentTimeMillis() - lastEventAt >= HEARTBEAT_TIMEOUT_MS
                if (stale) reconnect() else start()
            }
            override fun onStop(owner: LifecycleOwner) = stop()
        })
    }

    fun start() {
        if (running.compareAndSet(false, true)) {
            connectionJob = scope.launch { connectionLoop() }
            scope.launch { flushLoop() }
        }
    }

    fun stop() {
        if (running.compareAndSet(true, false)) {
            connectionJob?.cancel()
            connectionJob = null
            scope.launch { store.dispatch(AppEvent.ServerConnected.let { AppEvent.Unknown(SseEvent(type = "stop")) }) }
        }
    }

    fun reconnect() {
        connectionJob?.cancel()
        running.set(true)
        connectionJob = scope.launch { connectionLoop() }
    }

    // ─── Connection loop ───────────────────────────────────────────────────────

    private suspend fun connectionLoop() {
        var attempt = 0
        while (running.get()) {
            val baseUrl = connectionProvider.active?.url
            if (baseUrl == null) {
                delay(2_000)
                continue
            }
            store.dispatch(AppEvent.Unknown(SseEvent(type = "connecting")))
            val connected = connectOnce(baseUrl)
            if (!running.get()) break
            val backoff = min(RECONNECT_DELAY_BASE_MS * (1L shl attempt), RECONNECT_DELAY_MAX_MS)
            Log.d(TAG, "SSE disconnected, retry in ${backoff}ms (attempt $attempt)")
            attempt = if (connected) 0 else (attempt + 1)
            delay(backoff)
        }
    }

    private suspend fun connectOnce(baseUrl: String): Boolean {
        val url = "$baseUrl/global/event"
        val request = Request.Builder().url(url).build()

        return suspendCancellableCoroutine { cont ->
            var eventSource: EventSource? = null
            val heartbeatJob = scope.launch {
                delay(HEARTBEAT_TIMEOUT_MS)
                Log.w(TAG, "SSE heartbeat timeout — forcing reconnect")
                eventSource?.cancel()
                if (cont.isActive) cont.resume(false) {}
            }

            val listener = object : EventSourceListener() {
                override fun onOpen(es: EventSource, response: Response) {
                    lastEventAt = System.currentTimeMillis()
                    heartbeatJob.cancel()
                    scope.launch {
                        store.dispatch(AppEvent.ServerConnected)
                        // Reset heartbeat as a repeating timer
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
                    val raw = SseEvent(id = id, type = type ?: "unknown",
                        properties = try {
                            ForgeJson.parseToJsonElement(data).jsonObject
                        } catch (_: Exception) {
                            kotlinx.serialization.json.JsonObject(emptyMap())
                        }
                    )
                    batchChannel.trySend(raw)
                }

                override fun onClosed(es: EventSource) {
                    heartbeatJob.cancel()
                    if (cont.isActive) cont.resume(true) {}
                }

                override fun onFailure(es: EventSource, t: Throwable?, response: Response?) {
                    heartbeatJob.cancel()
                    Log.w(TAG, "SSE failure: ${t?.message}")
                    if (cont.isActive) cont.resume(false) {}
                }
            }

            eventSource = EventSources.createFactory(client).newEventSource(request, listener)

            cont.invokeOnCancellation {
                heartbeatJob.cancel()
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
                val event = eventParser.parse(raw)
                store.dispatch(event)
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
                    val partId = event.properties["id"]?.jsonPrimitive?.content ?: ""
                    val key = "message.part.updated:$partId"
                    latestByKey[key] = event
                    updatedPartIds.add(partId)
                }
                "message.part.delta" -> {
                    val partId = event.properties["id"]?.jsonPrimitive?.content ?: ""
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
