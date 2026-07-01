package dev.opcode42.core.data

import dev.opcode42.core.sdk.BaseUrlProvider
import dev.opcode42.core.sdk.HttpTransport
import dev.opcode42.core.sdk.Opcode42Client
import dev.opcode42.core.store.AppStore
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/**
 * Integration-style tests for the cross-project session fan-out that moved out of
 * `SessionListViewModel` into [DefaultSessionRepository.refreshAll]. Drives the real REST client
 * against a [MockWebServer] and asserts the resulting [AppStore] state.
 */
class DefaultSessionRepositoryTest {

    private lateinit var server: MockWebServer
    private lateinit var store: AppStore
    private lateinit var repo: DefaultSessionRepository

    @Before fun setUp() {
        server = MockWebServer()
        server.start()
        val baseUrl = object : BaseUrlProvider {
            override val baseUrl = server.url("/").toString().trimEnd('/')
        }
        store = AppStore()
        repo = DefaultSessionRepository(Opcode42Client(HttpTransport(OkHttpClient(), OkHttpClient(), baseUrl)), store)
    }

    @After fun tearDown() { server.shutdown() }

    @Test fun refreshAll_aggregatesAndDedupesAcrossProjectsAndGlobal() = runTest {
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val url = request.requestUrl!!
                return when {
                    url.encodedPath == "/project" -> MockResponse().setBody(
                        """[{"id":"p1","worktree":"/a"},{"id":"p2","worktree":"/b"}]""",
                    )
                    url.encodedPath == "/session" && url.queryParameter("directory") == "/a" ->
                        MockResponse().setBody("""[{"id":"s1","directory":"/a"},{"id":"s2","directory":"/a"}]""")
                    url.encodedPath == "/session" && url.queryParameter("directory") == "/b" ->
                        // s2 overlaps with /a — must be deduped by id.
                        MockResponse().setBody("""[{"id":"s2","directory":"/b"},{"id":"s3","directory":"/b"}]""")
                    url.encodedPath == "/session" -> MockResponse().setBody("[]") // global (no directory)
                    else -> MockResponse().setResponseCode(404)
                }
            }
        }

        val result = repo.refreshAll()

        assertTrue(result.isSuccess)
        // Store keeps sessions sorted by id; s2 appears once.
        assertEquals(listOf("s1", "s2", "s3"), store.state.value.sessions.map { it.id })
    }

    @Test fun refreshAll_oneDeadDirectoryDoesNotBlankTheList() = runTest {
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val url = request.requestUrl!!
                return when {
                    url.encodedPath == "/project" -> MockResponse().setBody(
                        """[{"id":"p1","worktree":"/a"},{"id":"p2","worktree":"/b"}]""",
                    )
                    url.encodedPath == "/session" && url.queryParameter("directory") == "/a" ->
                        MockResponse().setBody("""[{"id":"s1","directory":"/a"}]""")
                    url.encodedPath == "/session" && url.queryParameter("directory") == "/b" ->
                        MockResponse().setResponseCode(500).setBody("boom") // unreachable directory
                    url.encodedPath == "/session" -> MockResponse().setBody("[]")
                    else -> MockResponse().setResponseCode(404)
                }
            }
        }

        val result = repo.refreshAll()

        // The whole call still succeeds and the healthy directory's sessions survive.
        assertTrue(result.isSuccess)
        assertEquals(listOf("s1"), store.state.value.sessions.map { it.id })
    }

    @Test fun refreshAll_failsWhenEveryQueryFails() = runTest {
        // Daemon unreachable: /project and the global /session both 500. With no project dirs, the
        // only query is the global one, which fails → the whole refresh is a failure (so the UI can
        // show a retry instead of a misleading empty list).
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse =
                MockResponse().setResponseCode(500).setBody("down")
        }

        val result = repo.refreshAll()

        assertTrue(result.isFailure)
        assertEquals(emptyList<String>(), store.state.value.sessions.map { it.id })
    }
}
