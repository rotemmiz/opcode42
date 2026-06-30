package dev.opcode42.core.network

import dev.opcode42.core.store.AppStore
import dev.opcode42.core.store.ConnectionState
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * Live end-to-end verification against a REAL opencode (or Opcode42) daemon.
 *
 * This is intentionally NOT part of the CI gate — it self-skips unless
 * `OPCODE_LIVE_OPENCODE` is set (the project keeps live-daemon/LLM flows as manual
 * verification, see .github/workflows/ci.yml). Run it against a daemon you control:
 *
 *     OPCODE_LIVE_OPENCODE=http://localhost:4096 \
 *       ./gradlew :core:network:testDebugUnitTest \
 *       --tests "dev.opcode42.core.network.SseLiveOpencodeTest" --rerun-tasks
 *
 * It exercises the exact production cold-start path — `SseManager.start()` opening
 * `/global/event` — then creates a session on the daemon over REST and asserts the
 * resulting `session.created` event travels the live SSE stream all the way into the
 * store. Before the cold-start flush-loop fix this assertion fails (the event is
 * received but never drained); after it, the new session appears.
 */
class SseLiveOpencodeTest {

    @Test
    fun `session created on the live daemon reaches the store via global SSE`() {
        val base = (System.getenv("OPCODE_LIVE_OPENCODE") ?: "").trim().removeSuffix("/")
        if (base.isEmpty()) {
            println("[SseLiveOpencodeTest] skipped — set OPCODE_LIVE_OPENCODE=http://localhost:4096 to run.")
            return
        }

        val client = OkHttpClient()
        val store = AppStore()
        val manager = SseManager(
            client = client,
            connectionProvider = LiveConnectionProvider(base),
            store = store,
            eventParser = SseEventParser(),
        )

        try {
            // Reproduce the historically-broken cold-start path: before the fix, onStart saw
            // lastEventAt == 0, judged the stream "stale", and brought the connection up via
            // reconnect() — which never started the flush loop. Driving reconnect() here makes
            // this a true live before/after: it fails against the daemon on the old code and
            // passes on the fixed code.
            manager.reconnect()

            runBlocking {
                val connected = withTimeoutOrNull(8_000) {
                    while (store.state.value.connectionState !is ConnectionState.Connected) delay(50)
                    true
                }
                assertEquals(true, connected, "never connected to $base/global/event")

                val directory = discoverDirectory(client, base)
                val before = store.state.value.sessions.map { it.id }.toSet()
                println("[SseLiveOpencodeTest] connected; ${before.size} sessions before; dir=$directory")

                val newId = createSession(client, base, directory)
                println("[SseLiveOpencodeTest] created $newId on the daemon; waiting for it on the SSE stream…")
                try {
                    val arrived = withTimeoutOrNull(10_000) {
                        while (store.state.value.sessions.none { it.id == newId }) delay(50)
                        true
                    }
                    assertEquals(
                        true,
                        arrived,
                        "session $newId created on the daemon never arrived in the store via /global/event — " +
                            "the live-update path is broken (flush loop not draining).",
                    )
                    println(
                        "[SseLiveOpencodeTest] PASS — store now has ${store.state.value.sessions.size} sessions; " +
                            "live SSE update reached the store.",
                    )
                } finally {
                    // Don't leave the session we created lingering on the live daemon (runs even
                    // if the assertion above fails).
                    deleteSession(client, base, newId, directory)
                }
            }
        } finally {
            manager.stop()
        }
    }

    /** Reads the directory opencode is serving from the first existing session. */
    private fun discoverDirectory(client: OkHttpClient, base: String): String {
        val body = client.newCall(Request.Builder().url("$base/session").build())
            .execute().use { it.body!!.string() }
        return Json.parseToJsonElement(body).jsonArray
            .firstOrNull()?.jsonObject?.get("directory")?.jsonPrimitive?.content
            ?: error("no existing session to discover the served directory from")
    }

    /** Creates a session over REST and returns its id. */
    private fun createSession(client: OkHttpClient, base: String, directory: String): String {
        val req = Request.Builder()
            .url("$base/session")
            .header("x-opencode-directory", directory)
            .post("{}".toRequestBody())
            .build()
        val body = client.newCall(req).execute().use { it.body!!.string() }
        return Json.parseToJsonElement(body).jsonObject["id"]!!.jsonPrimitive.content
    }

    /** Best-effort deletes the session this test created so it doesn't linger on the live daemon. */
    private fun deleteSession(client: OkHttpClient, base: String, id: String, directory: String) {
        runCatching {
            val req = Request.Builder()
                .url("$base/session/$id")
                .header("x-opencode-directory", directory)
                .delete()
                .build()
            client.newCall(req).execute().use { }
        }
    }

    private class LiveConnectionProvider(url: String) : ActiveConnectionProvider {
        private val flow = MutableStateFlow<ServerConnectionConfig?>(
            ServerConnectionConfig(url = url, http = HttpConfig(url = url)),
        )
        override val active: ServerConnectionConfig? get() = flow.value
        override val activeFlow = flow
    }
}
