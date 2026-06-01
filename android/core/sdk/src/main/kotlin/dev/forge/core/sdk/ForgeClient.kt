package dev.forge.core.sdk

import dev.forge.core.model.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
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
            arr.mapNotNull { elem ->
                try {
                    val obj = elem.jsonObject
                    // opencode wraps messages as { info: {...}, parts: [...] }
                    val info = obj["info"]?.jsonObject ?: obj   // fall back to flat if unwrapped
                    val partsArr = obj["parts"]?.jsonArray ?: JsonArray(emptyList())
                    val role = info["role"]?.jsonPrimitive?.content ?: "assistant"
                    val id = info["id"]?.jsonPrimitive?.content ?: return@mapNotNull null
                    val sessionID = info["sessionID"]?.jsonPrimitive?.content ?: sessionId
                    val timeObj = info["time"]?.jsonObject
                    val time = MessageTime(
                        created = timeObj?.get("created")?.jsonPrimitive?.long ?: 0L,
                        completed = timeObj?.get("completed")?.jsonPrimitive?.longOrNull,
                    )
                    val parts = partsArr.mapNotNull { p ->
                        try { parsePart(p.jsonObject) } catch (_: Exception) { null }
                    }
                    // opencode sends these flat on the message info, not nested.
                    val modelID = info["modelID"]?.jsonPrimitive?.contentOrNull
                    val providerID = info["providerID"]?.jsonPrimitive?.contentOrNull
                    val mode = info["mode"]?.jsonPrimitive?.contentOrNull
                    val agent = info["agent"]?.jsonPrimitive?.contentOrNull
                    val error = if (role == "assistant") {
                        info["error"]?.let { ForgeJson.decodeFromJsonElement(AssistantError.serializer(), it) }
                    } else null
                    val isSummary = role == "assistant" &&
                        info["summary"]?.jsonPrimitive?.booleanOrNull == true
                    Message(id = id, sessionID = sessionID, role = role, time = time,
                        parts = parts, error = error, modelID = modelID,
                        providerID = providerID, mode = mode, agent = agent,
                        isSummary = isSummary)
                } catch (_: Exception) { null }
            }
        }

    private fun parsePart(obj: JsonObject): Part {
        val type = obj["type"]?.jsonPrimitive?.content ?: "unknown"
        val id = obj["id"]?.jsonPrimitive?.content ?: ""
        val sessionID = obj["sessionID"]?.jsonPrimitive?.content ?: ""
        val messageID = obj["messageID"]?.jsonPrimitive?.content ?: ""
        return when (type) {
            "text" -> ForgeJson.decodeFromJsonElement(TextPart.serializer(), obj)
            "reasoning" -> ForgeJson.decodeFromJsonElement(ReasoningPart.serializer(), obj)
            "file" -> ForgeJson.decodeFromJsonElement(FilePart.serializer(), obj)
            "tool" -> ForgeJson.decodeFromJsonElement(ToolPart.serializer(), obj)
            "patch" -> ForgeJson.decodeFromJsonElement(PatchPart.serializer(), obj)
            "step-start" -> StepStartPart(id, sessionID, messageID)
            "step-finish" -> StepFinishPart(id, sessionID, messageID)
            else -> UnknownPart(id, sessionID, messageID, type)
        }
    }

    suspend fun sendPrompt(
        sessionId: String,
        text: String,
        directory: String? = null,
        attachments: List<FilePartInput> = emptyList(),
        model: ModelRef? = null,
        agent: String? = null,
    ) = post(
        path = "/session/$sessionId/message",
        body = buildJsonObject {
            model?.let {
                put("model", buildJsonObject {
                    put("providerID", it.providerID)
                    put("modelID", it.modelID)
                })
            }
            agent?.let { put("agent", it) }
            put("parts", buildJsonArray {
                if (text.isNotBlank()) {
                    add(buildJsonObject {
                        put("type", "text")
                        put("text", text)
                    })
                }
                attachments.forEach { att ->
                    add(buildJsonObject {
                        put("type", "file")
                        put("mime", att.mime)
                        put("url", att.url)
                    })
                }
            })
        },
        directory = directory,
    ) { _ -> Unit }

    // ─── Commands / file search ─────────────────────────────────────────────────

    /** GET /command — available slash commands for the directory. */
    suspend fun listCommands(directory: String? = null): List<CommandInfo> =
        get("/command", directory = directory) { json ->
            val arr = json as? JsonArray ?: return@get emptyList()
            arr.mapNotNull {
                try { ForgeJson.decodeFromJsonElement(CommandInfo.serializer(), it) } catch (_: Exception) { null }
            }
        }

    /** GET /provider — providers and their models, for the model picker. */
    suspend fun listProviders(directory: String? = null): ProvidersResponse =
        get("/provider", directory = directory) { json ->
            try {
                ForgeJson.decodeFromJsonElement(ProvidersResponse.serializer(), json)
            } catch (_: Exception) {
                ProvidersResponse()
            }
        }

    /** GET /agent — selectable agents (modes), for the agent picker. */
    suspend fun listAgents(directory: String? = null): List<AgentInfo> =
        get("/agent", directory = directory) { json ->
            val arr = json as? JsonArray ?: return@get emptyList()
            arr.mapNotNull {
                try { ForgeJson.decodeFromJsonElement(AgentInfo.serializer(), it) } catch (_: Exception) { null }
            }
        }

    /** GET /find/file?query= — fuzzy file paths for @-mentions. */
    suspend fun findFiles(query: String, directory: String? = null, limit: Int = 20): List<String> =
        withContext(Dispatchers.IO) {
            val base = baseUrl?.trimEnd('/') ?: error("No server configured")
            val params = buildString {
                append("?query=").append(URLEncoder.encode(query, "UTF-8"))
                append("&limit=").append(limit)
                directory?.let { append("&directory=").append(URLEncoder.encode(it, "UTF-8")) }
            }
            val req = Request.Builder().url("$base/find/file$params").also {
                directory?.let { d -> it.header("X-Opencode-Directory", URLEncoder.encode(d, "UTF-8")) }
            }.get().build()
            httpClient.newCall(req).execute().use { resp ->
                val body = resp.body?.string() ?: "[]"
                if (!resp.isSuccessful) error("HTTP ${resp.code} for GET /find/file: $body")
                val arr = ForgeJson.parseToJsonElement(body) as? JsonArray ?: return@use emptyList()
                arr.mapNotNull { it.jsonPrimitive.contentOrNull }
            }
        }

    /** POST /session/{id}/command — run a slash command with arguments. */
    suspend fun runCommand(
        sessionId: String,
        command: String,
        arguments: String,
        directory: String? = null,
    ) = post(
        path = "/session/$sessionId/command",
        body = buildJsonObject {
            put("command", command)
            put("arguments", arguments)
        },
        directory = directory,
    ) { _ -> Unit }

    // ─── PTY ──────────────────────────────────────────────────────────────────

    /**
     * Creates a new PTY session on the server.
     * POST /pty with {"directory": dir} → PtyInfo
     */
    suspend fun createPty(directory: String): PtyInfo = post(
        path = "/pty",
        body = buildJsonObject { put("directory", directory) },
    ) { json ->
        val obj = json.jsonObject
        PtyInfo(
            id = obj["id"]?.jsonPrimitive?.content ?: error("PTY response missing id"),
            title = obj["title"]?.jsonPrimitive?.contentOrNull,
            status = obj["status"]?.jsonPrimitive?.content ?: "running",
        )
    }

    /**
     * Opens a WebSocket connection to the PTY session and returns a [PtyClient].
     * ws://host/pty/{ptyId}/connect?auth_token=<base64>
     * The auth_token is base64(user:pass) or base64(:token).
     */
    fun connectPty(ptyId: String, authToken: String): PtyClient {
        val base = baseUrl?.trimEnd('/') ?: error("No server configured")
        // Replace http(s):// with ws(s)://
        val wsBase = base
            .replace(Regex("^https://"), "wss://")
            .replace(Regex("^http://"), "ws://")
        val url = "$wsBase/pty/$ptyId/connect?auth_token=${URLEncoder.encode(authToken, "UTF-8")}"
        val channel = Channel<ByteArray>(Channel.UNLIMITED)
        val listener = PtyClient.createListener(channel)
        val request = Request.Builder().url(url).build()
        val ws = httpClient.newWebSocket(request, listener)
        return PtyClient(ws, channel)
    }

    // ─── Diff ─────────────────────────────────────────────────────────────────

    suspend fun getSessionDiff(
        sessionId: String,
        messageId: String,
        directory: String,
    ): List<SnapshotFileDiff> = withContext(Dispatchers.IO) {
        val base = baseUrl?.trimEnd('/') ?: error("No server configured")
        val url = "$base/session/$sessionId/diff" +
            "?messageID=${URLEncoder.encode(messageId, "UTF-8")}" +
            "&directory=${URLEncoder.encode(directory, "UTF-8")}"
        val req = Request.Builder().url(url)
            .header("X-Opencode-Directory", URLEncoder.encode(directory, "UTF-8"))
            .get()
            .build()
        httpClient.newCall(req).execute().use { resp ->
            val body = resp.body?.string() ?: "[]"
            if (!resp.isSuccessful) error("HTTP ${resp.code} for GET diff: $body")
            val arr = ForgeJson.parseToJsonElement(body) as? JsonArray ?: return@use emptyList()
            arr.map { ForgeJson.decodeFromJsonElement(SnapshotFileDiff.serializer(), it) }
        }
    }

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

    // ─── Session fork / delete ────────────────────────────────────────────────

    suspend fun forkSession(sessionId: String): Session = post(
        path = "/session/$sessionId/fork",
        body = JsonObject(emptyMap()),
    ) { json -> ForgeJson.decodeFromJsonElement(Session.serializer(), json) }

    suspend fun deleteSession(sessionId: String) = delete("/session/$sessionId")

    /** POST /session/{id}/abort — interrupt a running agent turn. Returns true if it was aborted. */
    suspend fun abortSession(sessionId: String, directory: String? = null): Boolean = post(
        path = "/session/$sessionId/abort",
        body = JsonObject(emptyMap()),
        directory = directory,
    ) { json -> (json as? JsonPrimitive)?.booleanOrNull ?: false }

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
        httpClient.newCall(req).execute().use { resp ->
            val body = resp.body?.string() ?: "{}"
            if (!resp.isSuccessful) error("HTTP ${resp.code} for GET $path: $body")
            parse(ForgeJson.parseToJsonElement(body))
        }
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
        httpClient.newCall(req).execute().use { resp ->
            val respBody = resp.body?.string() ?: "{}"
            if (!resp.isSuccessful) error("HTTP ${resp.code} for POST $path: $respBody")
            val elem = try { ForgeJson.parseToJsonElement(respBody) } catch (_: Exception) { JsonObject(emptyMap()) }
            parse(elem)
        }
    }

    private suspend fun delete(path: String): Unit = withContext(Dispatchers.IO) {
        val url = buildUrl(path, null)
        val req = Request.Builder().url(url).delete().build()
        httpClient.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) {
                val body = resp.body?.string() ?: ""
                error("HTTP ${resp.code} for DELETE $path: $body")
            }
        }
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
