package dev.opcode42.core.sdk

import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test

/**
 * Pins the wire contract for `POST /session/{id}/revert` (openapi.json:7539,
 * `session.revert`) against a [MockWebServer]: the request body shape
 * `{ messageID, partID? }` (required: `["messageID"]`, `additionalProperties: false`)
 * and the `Session` response decode. The plan's `/timeline` command uses this
 * endpoint to "Revert to here" — the plan author used "rewind" colloquially;
 * opencode exposes `/revert`, which is the intended endpoint.
 */
class RevertWireTest {

    private lateinit var server: MockWebServer
    private lateinit var client: Opcode42Client
    private lateinit var baseUrl: MutableBaseUrl

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        baseUrl = MutableBaseUrl(server.url("/").toString().trimEnd('/'))
        client = Opcode42Client(HttpTransport(OkHttpClient(), OkHttpClient(), baseUrl))
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    @Test
    fun revertEmitsMessageIdOnlyWhenPartIdAbsent() = runTest {
        server.enqueue(MockResponse().setBody("""{"id":"ses_1","title":"Reverted"}"""))

        val session = client.revertSession("ses_1", "msg_7")

        assertEquals("ses_1", session.id)
        assertEquals("Reverted", session.title)

        val req = server.takeRequest()
        assertEquals("POST", req.method)
        assertEquals("/session/ses_1/revert", req.path)
        val body = Json.parseToJsonElement(req.body.readUtf8()).jsonObject
        assertEquals("msg_7", body["messageID"]?.jsonPrimitive?.content)
        assertNull(body["partID"])
    }

    @Test
    fun revertIncludesPartIdWhenProvided() = runTest {
        server.enqueue(MockResponse().setBody("""{"id":"ses_1"}"""))

        client.revertSession("ses_1", "msg_7", partId = "prt_3")

        val body = Json.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
        assertEquals("msg_7", body["messageID"]?.jsonPrimitive?.content)
        assertEquals("prt_3", body["partID"]?.jsonPrimitive?.content)
    }

    @Test
    fun revertSendsDirectoryHeaderWhenProvided() = runTest {
        server.enqueue(MockResponse().setBody("""{"id":"ses_1"}"""))

        client.revertSession("ses_1", "msg_7", directory = "/repo")

        val req = server.takeRequest()
        // The transport encodes the header value (enc() → %20 for spaces, %2F for slashes)
        // so the daemon's url.PathUnescape decodes it correctly.
        assertEquals("%2Frepo", req.getHeader("X-Opencode-Directory"))
    }

    private class MutableBaseUrl(var value: String?) : BaseUrlProvider {
        override val baseUrl: String? get() = value
    }
}