package dev.forge.core.network

import dev.forge.core.store.AppStore
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * Regression test for the cold-start "client never live-updates" bug.
 *
 * The SSE plumbing connects and `onEvent` buffers every frame into [SseManager]'s `batchChannel`,
 * but the only consumer that drains that channel — `flushLoop` — is what parses each event and
 * dispatches it to the [AppStore]. Before the fix, the cold-start path (`reconnect()`, reached on
 * first foreground because `lastEventAt == 0` made the heartbeat look "stale") started the
 * connection loop but never the flush loop, so events piled up undrained and the UI never updated.
 *
 * These tests assert that an SSE `session.status` frame — which can ONLY influence
 * [AppState.sessionStatus] by travelling all the way through `flushLoop` → `SseEventParser` →
 * `store.dispatch` → `reduce` — actually reaches the store when the connection is brought up via
 * each public entry point. (`server.connected`/connection-state events are dispatched directly from
 * `onOpen` and would pass even with a dead flush loop, so they are deliberately not used as probes.)
 */
class SseColdStartTest {

    private val sessionStatusFrame =
        "id: evt_1\n" +
            "event: message\n" +
            "data: {\"id\":\"evt_1\",\"type\":\"session.status\"," +
            "\"properties\":{\"sessionID\":\"ses_test\",\"status\":{\"type\":\"running\"}}}\n\n"

    @Test
    fun `reconnect drains SSE events to the store (cold-start path)`() {
        assertEventReachesStore { it.reconnect() }
    }

    @Test
    fun `start drains SSE events to the store`() {
        assertEventReachesStore { it.start() }
    }

    /**
     * Stands up a MockWebServer that streams the [sessionStatusFrame] for every request (so the
     * manager's reconnect loop keeps getting served), brings the connection up via [bringUp], and
     * asserts the parsed `session.status` reaches the store within a generous timeout.
     */
    private fun assertEventReachesStore(bringUp: (SseManager) -> Unit) {
        val server = MockWebServer().apply {
            dispatcher = object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse =
                    MockResponse()
                        .setHeader("Content-Type", "text/event-stream")
                        .setBody(sessionStatusFrame)
            }
            start()
        }
        val baseUrl = server.url("/").toString().removeSuffix("/")
        val store = AppStore()
        val manager = SseManager(
            client = OkHttpClient(),
            connectionProvider = FakeConnectionProvider(baseUrl),
            store = store,
            eventParser = SseEventParser(),
        )

        try {
            bringUp(manager)
            val arrived = runBlocking {
                withTimeoutOrNull(5_000) {
                    while (store.state.value.sessionStatus["ses_test"] != "running") {
                        delay(20)
                    }
                    true
                }
            }
            assertEquals(
                true,
                arrived,
                "SSE session.status never reached the store — the batch/flush loop is not draining " +
                    "batchChannel after the connection was brought up.",
            )
        } finally {
            manager.stop()
            server.shutdown()
        }
    }

    private class FakeConnectionProvider(url: String) : ActiveConnectionProvider {
        private val flow = MutableStateFlow<ServerConnectionConfig?>(
            ServerConnectionConfig(url = url, http = HttpConfig(url = url)),
        )
        override val active: ServerConnectionConfig? get() = flow.value
        override val activeFlow = flow
    }
}
