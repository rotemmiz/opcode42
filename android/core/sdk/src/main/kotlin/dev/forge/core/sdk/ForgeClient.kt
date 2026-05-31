package dev.forge.core.sdk

import dev.forge.core.model.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.net.URLEncoder
import javax.inject.Inject
import javax.inject.Singleton

private val JSON_MEDIA = "application/json; charset=utf-8".toMediaType()

/**
 * Hand-written REST client wrapping OkHttp.
 * Handles auth via the injected OkHttpClient's AuthInterceptor.
 * Injects X-Opencode-Directory header per plan 06.
 */
@Singleton
class ForgeClient @Inject constructor(
    private val httpClient: OkHttpClient,
    private val baseUrlProvider: BaseUrlProvider,
) {
    private val baseUrl get() = baseUrlProvider.baseUrl

    // ─── Session ──────────────────────────────────────────────────────────────

    suspend fun listSessions(): List<Session> = get("/session") { json ->
        val arr = json as? JsonArray ?: return@get emptyList()
        arr.map { ForgeJson.decodeFromJsonElement(Session.serializer(), it) }
    }

    suspend fun createSession(directory: String? = null): Session = post(
        path = "/session",
        body = buildJsonObject { directory?.let { put("directory", it) } },
        directory = directory,
    ) { json -> ForgeJson.decodeFromJsonElement(Session.serializer(), json) }

    suspend fun getMessages(sessionId: String, directory: String? = null): List<Message> =
        get("/session/$sessionId/message", directory = directory) { json ->
            val arr = json as? JsonArray ?: return@get emptyList()
            arr.map { elem ->
                val obj = elem.jsonObject
                val role = obj["role"]?.jsonPrimitive?.content ?: "assistant"
                when (role) {
                    "user" -> ForgeJson.decodeFromJsonElement(UserMessage.serializer(), obj).toMessage()
                    else -> ForgeJson.decodeFromJsonElement(AssistantMessage.serializer(), obj).toMessage()
                }
            }
        }

    suspend fun sendPrompt(sessionId: String, text: String, directory: String? = null) =
        post(
            path = "/session/$sessionId/message",
            body = buildJsonObject { put("text", text) },
            directory = directory,
        ) { _ -> Unit }

    // ─── Permission ───────────────────────────────────────────────────────────

    suspend fun replyPermission(requestId: String, allow: Boolean) =
        post(
            path = "/permission/$requestId/reply",
            body = buildJsonObject { put("allow", allow) },
        ) { _ -> Unit }

    // ─── Question ─────────────────────────────────────────────────────────────

    suspend fun replyQuestion(requestId: String, answer: String) =
        post(
            path = "/question/$requestId/reply",
            body = buildJsonObject { put("answer", answer) },
        ) { _ -> Unit }

    suspend fun rejectQuestion(requestId: String) =
        post(
            path = "/question/$requestId/reject",
            body = JsonObject(emptyMap()),
        ) { _ -> Unit }

    // ─── HTTP helpers ─────────────────────────────────────────────────────────

    private suspend fun <T> get(
        path: String,
        directory: String? = null,
        parse: (JsonElement) -> T,
    ): T = withContext(Dispatchers.IO) {
        val url = buildUrl(path, directory)
        val req = Request.Builder().url(url).get().also {
            directory?.let { d -> it.header("X-Opencode-Directory", URLEncoder.encode(d, "UTF-8")) }
        }.build()
        val resp = httpClient.newCall(req).execute()
        val body = resp.body?.string() ?: "{}"
        if (!resp.isSuccessful) error("HTTP ${resp.code} for GET $path: $body")
        parse(ForgeJson.parseToJsonElement(body))
    }

    private suspend fun <T> post(
        path: String,
        body: JsonObject,
        directory: String? = null,
        parse: (JsonElement) -> T,
    ): T = withContext(Dispatchers.IO) {
        val url = buildUrl(path, null)
        val reqBody = body.toString().toRequestBody(JSON_MEDIA)
        val req = Request.Builder().url(url).post(reqBody).also {
            directory?.let { d -> it.header("X-Opencode-Directory", URLEncoder.encode(d, "UTF-8")) }
        }.build()
        val resp = httpClient.newCall(req).execute()
        val respBody = resp.body?.string() ?: "{}"
        if (!resp.isSuccessful) error("HTTP ${resp.code} for POST $path: $respBody")
        val elem = try { ForgeJson.parseToJsonElement(respBody) } catch (_: Exception) { JsonObject(emptyMap()) }
        parse(elem)
    }

    private fun buildUrl(path: String, directory: String?): String {
        val base = baseUrl?.trimEnd('/') ?: error("No server configured")
        return if (directory != null) {
            "$base$path?directory=${URLEncoder.encode(directory, "UTF-8")}"
        } else {
            "$base$path"
        }
    }
}

interface BaseUrlProvider {
    val baseUrl: String?
}
