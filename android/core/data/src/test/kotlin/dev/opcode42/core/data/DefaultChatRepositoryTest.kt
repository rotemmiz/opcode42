package dev.opcode42.core.data

import dev.opcode42.core.network.ActiveConnectionProvider
import dev.opcode42.core.network.ServerConnectionConfig
import dev.opcode42.core.network.SseEventParser
import dev.opcode42.core.network.SseManager
import dev.opcode42.core.sdk.BaseUrlProvider
import dev.opcode42.core.sdk.HttpTransport
import dev.opcode42.core.sdk.Opcode42Client
import dev.opcode42.core.store.AppStore
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/**
 * Tests the optimistic-send rollback and the idempotent diff fetch that moved out of
 * `ChatViewModel` into [DefaultChatRepository]. Real REST client over a [MockWebServer] + real
 * [AppStore]; the [SseManager] is constructed but never started (no method under test touches it).
 */
class DefaultChatRepositoryTest {

    private lateinit var server: MockWebServer
    private lateinit var store: AppStore
    private lateinit var repo: DefaultChatRepository

    private class NoConnection : ActiveConnectionProvider {
        override val active: ServerConnectionConfig? = null
        override val activeFlow: StateFlow<ServerConnectionConfig?> = MutableStateFlow(null)
    }

    @Before fun setUp() {
        server = MockWebServer()
        server.start()
        val baseUrl = object : BaseUrlProvider {
            override val baseUrl = server.url("/").toString().trimEnd('/')
        }
        store = AppStore()
        val client = Opcode42Client(HttpTransport(OkHttpClient(), OkHttpClient(), baseUrl))
        val sse = SseManager(OkHttpClient(), NoConnection(), store, SseEventParser())
        repo = DefaultChatRepository(client, store, sse)
    }

    @After fun tearDown() { server.shutdown() }

    @Test fun send_keepsOptimisticBubbleOnSuccess() = runTest {
        server.enqueue(MockResponse().setBody("{}"))

        val result = repo.send("ses1", "hello", directory = null, attachments = emptyList(), model = null, agent = null)

        assertTrue(result.isSuccess)
        // The optimistic bubble stays until the server echoes the user message back over SSE/REST.
        val optimistic = store.state.value.optimisticMessages["ses1"].orEmpty()
        assertEquals(1, optimistic.size)
        assertEquals("hello", optimistic.first().text)
    }

    @Test fun send_rollsBackOptimisticBubbleOnFailure() = runTest {
        server.enqueue(MockResponse().setResponseCode(500).setBody("boom"))

        val result = repo.send("ses1", "hello", directory = null, attachments = emptyList(), model = null, agent = null)

        assertTrue(result.isFailure)
        assertTrue(store.state.value.optimisticMessages["ses1"].orEmpty().isEmpty())
    }

    @Test fun loadDiff_isIdempotent_secondCallHitsNoNetwork() = runTest {
        server.enqueue(MockResponse().setBody("[]")) // only one diff response is provided

        repo.loadDiff("ses1", "msg1", "/dir")
        // Second call sees msg1 already in store.diffs and must short-circuit (no second request).
        repo.loadDiff("ses1", "msg1", "/dir")

        assertEquals(1, server.requestCount)
        assertTrue(store.state.value.diffs.containsKey("msg1"))
    }

    @Test fun loadDiff_storesEmptyOnFailureToStopRetries() = runTest {
        server.enqueue(MockResponse().setResponseCode(500).setBody("nope"))

        val result = repo.loadDiff("ses1", "msg1", "/dir")

        assertTrue(result.isFailure)
        // An empty entry is recorded so the auto-loader won't retry this message forever.
        assertTrue(store.state.value.diffs.containsKey("msg1"))
        assertFalse(store.state.value.diffs["msg1"]!!.isNotEmpty())
    }
}
