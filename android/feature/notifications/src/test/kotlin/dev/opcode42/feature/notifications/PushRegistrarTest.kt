package dev.opcode42.feature.notifications

import dev.opcode42.core.sdk.BaseUrlProvider
import dev.opcode42.core.sdk.Opcode42Client
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
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
 * Exercises [PushRegistrar] (and the `POST|DELETE /push/register` body it sends
 * via [Opcode42Client]) against a [MockWebServer], with fakes for FCM token and the
 * identity store — no Firebase / Android framework. Pins the wire contract to
 * `internal/server/push_handlers.go` pushRegisterInput.
 */
class PushRegistrarTest {

    private lateinit var server: MockWebServer
    private lateinit var registrar: PushRegistrar
    private lateinit var prefs: FakeIdentityStore
    private lateinit var tokenProvider: FakeTokenProvider
    private lateinit var baseUrl: MutableBaseUrl

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        prefs = FakeIdentityStore(deviceId = "device-123")
        tokenProvider = FakeTokenProvider(token = "tok-A")
        baseUrl = MutableBaseUrl(server.url("/").toString().trimEnd('/'))
        val client = Opcode42Client(OkHttpClient(), baseUrl)
        registrar = PushRegistrar(client, prefs, tokenProvider, baseUrl)
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    @Test
    fun syncRegistersTokenAndSendsExpectedBody() = runTest {
        server.enqueue(MockResponse().setBody("true"))

        val didRegister = registrar.sync()

        assertTrue(didRegister)
        val req = server.takeRequest()
        assertEquals("POST", req.method)
        assertEquals("/push/register", req.path)
        val body = Json.parseToJsonElement(req.body.readUtf8()).jsonObject
        assertEquals("device-123", body["device_id"]?.jsonPrimitive?.content)
        assertEquals("tok-A", body["fcm_token"]?.jsonPrimitive?.content)
        assertEquals("android", body["platform"]?.jsonPrimitive?.content)
        assertEquals("tok-A", prefs.registeredToken())
    }

    @Test
    fun syncIsNoOpWhenTokenUnchanged() = runTest {
        prefs.setRegisteredToken("tok-A")

        val didRegister = registrar.sync()

        assertFalse(didRegister)
        assertEquals(0, server.requestCount)
    }

    @Test
    fun syncReRegistersWhenTokenChanged() = runTest {
        prefs.setRegisteredToken("tok-OLD")
        server.enqueue(MockResponse().setBody("true"))

        val didRegister = registrar.sync()

        assertTrue(didRegister)
        assertEquals("tok-A", prefs.registeredToken())
    }

    @Test
    fun syncIsNoOpWithoutActiveServer() = runTest {
        baseUrl.value = null

        val didRegister = registrar.sync()

        assertFalse(didRegister)
        assertEquals(0, server.requestCount)
    }

    @Test
    fun onTokenRefreshedRegistersNewToken() = runTest {
        server.enqueue(MockResponse().setBody("true"))

        registrar.onTokenRefreshed("tok-NEW")

        val req = server.takeRequest()
        val body = Json.parseToJsonElement(req.body.readUtf8()).jsonObject
        assertEquals("tok-NEW", body["fcm_token"]?.jsonPrimitive?.content)
        assertEquals("tok-NEW", prefs.registeredToken())
    }

    @Test
    fun onTokenRefreshedWithoutServerClearsMarkerForNextSync() = runTest {
        prefs.setRegisteredToken("tok-A")
        baseUrl.value = null

        registrar.onTokenRefreshed("tok-NEW")

        assertNull(prefs.registeredToken())
        assertEquals(0, server.requestCount)
    }

    @Test
    fun unregisterSendsDeleteAndClearsMarker() = runTest {
        prefs.setRegisteredToken("tok-A")
        server.enqueue(MockResponse().setBody("true"))

        registrar.unregister()

        val req = server.takeRequest()
        assertEquals("DELETE", req.method)
        assertEquals("/push/register/device-123", req.path)
        assertNull(prefs.registeredToken())
    }

    @Test
    fun unregisterTreats404AsSuccess() = runTest {
        prefs.setRegisteredToken("tok-A")
        server.enqueue(MockResponse().setResponseCode(404).setBody("{}"))

        registrar.unregister()

        assertNull(prefs.registeredToken())
    }

    @Test
    fun unregisterClearsMarkerWithoutServer() = runTest {
        prefs.setRegisteredToken("tok-A")
        baseUrl.value = null

        registrar.unregister()

        assertNull(prefs.registeredToken())
        assertEquals(0, server.requestCount)
    }

    @Test
    fun registerEncodesSessionFilterArrayWhenProvided() = runTest {
        // Direct client check: session_filter is omitted by default and encoded as
        // a JSON array when present (matches pushRegisterInput.session_filter).
        server.enqueue(MockResponse().setBody("true"))
        val client = Opcode42Client(OkHttpClient(), baseUrl)

        client.registerPush(
            deviceId = "d",
            fcmToken = "t",
            sessionFilter = listOf("ses_1", "ses_2"),
        )

        val body = Json.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
        val filter = body["session_filter"]!!.jsonArray
        assertEquals(2, filter.size)
        assertEquals("ses_1", filter[0].jsonPrimitive.content)
    }

    private class FakeIdentityStore(private val deviceId: String) : PushIdentityStore {
        private var token: String? = null
        override suspend fun deviceId(): String = deviceId
        override suspend fun registeredToken(): String? = token
        override suspend fun setRegisteredToken(token: String?) {
            this.token = token
        }
    }

    private class FakeTokenProvider(private val token: String?) : PushTokenProvider {
        override suspend fun currentToken(): String? = token
    }

    private class MutableBaseUrl(var value: String?) : BaseUrlProvider {
        override val baseUrl: String? get() = value
    }
}
