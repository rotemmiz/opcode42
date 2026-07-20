package dev.opcode42.core.sdk

import dev.opcode42.core.model.PermissionRequest
import dev.opcode42.core.model.QuestionRequest
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/**
 * Pins the wire contract for the permission/question REST surface against a
 * [MockWebServer]: the JSON body [Opcode42Client.replyPermission] emits (the
 * `{ reply, message? }` shape from `packages/schema/src/v1/permission.ts`
 * `PermissionReplyBody`, not the legacy `{ allow }`), and the `GET /permission`
 * / `GET /question` list decode. Matches `openapi.json`
 * `POST /permission/{requestID}/reply` (`required: ["reply"]`,
 * `additionalProperties: false`) and `GET /permission` → `permission.list`.
 */
class PermissionWireTest {

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
    fun replyPermissionEmitsReplyBodyWithoutAllowKey() = runTest {
        server.enqueue(MockResponse().setBody("{}"))

        client.replyPermission("per_1", "always")

        val req = server.takeRequest()
        assertEquals("POST", req.method)
        assertEquals("/permission/per_1/reply", req.path)
        val body = Json.parseToJsonElement(req.body.readUtf8()).jsonObject
        assertEquals("always", body["reply"]?.jsonPrimitive?.content)
        assertNull(body["allow"])
        assertNull(body["message"])
    }

    @Test
    fun replyPermissionOnceOmitsMessage() = runTest {
        server.enqueue(MockResponse().setBody("{}"))

        client.replyPermission("per_1", "once")

        val body = Json.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
        assertEquals("once", body["reply"]?.jsonPrimitive?.content)
        assertNull(body["message"])
    }

    @Test
    fun replyPermissionRejectOmitsMessage() = runTest {
        server.enqueue(MockResponse().setBody("{}"))

        client.replyPermission("per_1", "reject")

        val body = Json.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
        assertEquals("reject", body["reply"]?.jsonPrimitive?.content)
        assertNull(body["message"])
    }

    @Test
    fun replyPermissionIncludesMessageWhenProvided() = runTest {
        server.enqueue(MockResponse().setBody("{}"))

        client.replyPermission("per_1", "once", message = "ok")

        val body = Json.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
        assertEquals("once", body["reply"]?.jsonPrimitive?.content)
        assertEquals("ok", body["message"]?.jsonPrimitive?.content)
    }

    @Test
    fun listPermissionsDecodesWireArray() = runTest {
        server.enqueue(
            MockResponse().setBody(
                """[
                  {"id":"per_1","sessionID":"ses_1","permission":"bash",
                   "patterns":["**"],"always":["**"],"metadata":{}},
                  {"id":"per_2","sessionID":"ses_2","permission":"read",
                   "patterns":["/tmp/*"],"always":[],"metadata":{"foo":"bar"},
                   "tool":{"messageID":"msg_9","callID":"call_9"}}
                ]""",
            ),
        )

        val list = client.listPermissions()

        assertEquals(2, list.size)
        val first = list[0]
        assertEquals("per_1", first.id)
        assertEquals("ses_1", first.sessionID)
        assertEquals("bash", first.permission)
        assertEquals(listOf("**"), first.patterns)
        assertEquals(listOf("**"), first.always)
        assertEquals("bash", first.title)
        assertEquals("**", first.description)
        val second = list[1]
        assertEquals("per_2", second.id)
        assertEquals("read", second.permission)
        assertEquals(listOf("/tmp/*"), second.patterns)
        assertEquals("msg_9", second.tool?.messageID)
        assertEquals("call_9", second.tool?.callID)

        val req = server.takeRequest()
        assertEquals("GET", req.method)
        assertEquals("/permission", req.path)
    }

    @Test
    fun listPermissionsToleratesNonArrayBody() = runTest {
        server.enqueue(MockResponse().setBody("{}"))

        val list = client.listPermissions()

        assertTrue(list.isEmpty())
    }

    @Test
    fun listPermissionsSkipsUnparseableEntries() = runTest {
        server.enqueue(
            MockResponse().setBody(
                """[
                  {"id":"per_1","sessionID":"ses_1","permission":"bash"},
                  {"id":"per_2"},
                  {"id":"per_3","sessionID":"ses_3","permission":"read","patterns":["a"]}
                ]""",
            ),
        )

        val list = client.listPermissions()

        // per_2 is missing required sessionID → skipped; per_1 and per_3 survive.
        assertEquals(2, list.size)
        assertEquals("per_1", list[0].id)
        assertEquals("per_3", list[1].id)
        assertEquals(listOf("a"), list[1].patterns)
    }

    @Test
    fun listQuestionsDecodesWireArray() = runTest {
        server.enqueue(
            MockResponse().setBody(
                """[
                  {"id":"qst_1","sessionID":"ses_1",
                   "questions":[{"question":"Continue?","header":"h","options":[{"label":"Yes"}]}],
                   "tool":{"messageID":"msg_1","callID":"call_1"}}
                ]""",
            ),
        )

        val list = client.listQuestions()

        assertEquals(1, list.size)
        val q = list[0]
        assertEquals("qst_1", q.id)
        assertEquals("ses_1", q.sessionID)
        assertEquals("Continue?", q.questions.first().question)
        assertEquals("Yes", q.questions.first().options.first().label)
        assertEquals("msg_1", q.tool?.messageID)
        assertEquals("call_1", q.tool?.callID)

        val req = server.takeRequest()
        assertEquals("GET", req.method)
        assertEquals("/question", req.path)
    }

    @Test
    fun listQuestionsToleratesNonArrayBody() = runTest {
        server.enqueue(MockResponse().setBody("{}"))

        val list = client.listQuestions()

        assertFalse(list.isNotEmpty())
        assertTrue(list.isEmpty())
    }

    private class MutableBaseUrl(var value: String?) : BaseUrlProvider {
        override val baseUrl: String? get() = value
    }
}