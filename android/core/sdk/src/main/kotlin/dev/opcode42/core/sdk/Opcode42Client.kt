package dev.opcode42.core.sdk

import dev.opcode42.core.model.*
import kotlinx.coroutines.channels.Channel
import kotlinx.serialization.json.*
import okhttp3.Request
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Hand-written REST client for the Opcode42/opencode daemon.
 *
 * This type is now just the *endpoint catalog*: each method builds a request body and maps the
 * response. HTTP plumbing (URL building, the `X-Opencode-Directory` header, IO dispatch, non-2xx
 * → [HttpException]) lives in [HttpTransport]; JSON→domain mapping lives in `Mappers.kt`.
 */
@Singleton
class Opcode42Client @Inject constructor(
    private val transport: HttpTransport,
) {
    // ─── Session ──────────────────────────────────────────────────────────────

    suspend fun listSessions(directory: String? = null): List<Session> =
        transport.get("/session", directory) { json ->
            val arr = json as? JsonArray ?: return@get emptyList()
            arr.map { Opcode42Json.decodeFromJsonElement(Session.serializer(), it) }
        }

    /**
     * `GET /project` — the daemon's projects, each with a worktree + sandboxes. Used to
     * enumerate every directory so the session list can aggregate across projects without
     * a configured working folder. Tolerates a non-array body (returns empty).
     */
    suspend fun listProjects(): List<Project> = transport.get("/project", null) { json ->
        val arr = json as? JsonArray ?: return@get emptyList()
        arr.mapNotNull { runCatching { Opcode42Json.decodeFromJsonElement(Project.serializer(), it) }.getOrNull() }
    }

    suspend fun createSession(directory: String? = null): Session = transport.post(
        path = "/session",
        body = buildJsonObject { directory?.let { put("directory", it) } },
        directory = directory,
    ) { json -> Opcode42Json.decodeFromJsonElement(Session.serializer(), json) }

    suspend fun getMessages(sessionId: String, directory: String? = null): List<Message> =
        transport.get("/session/$sessionId/message", directory = directory) { json ->
            val arr = json as? JsonArray ?: return@get emptyList()
            arr.mapNotNull { parseMessage(it, sessionId) }
        }

    suspend fun sendPrompt(
        sessionId: String,
        text: String,
        directory: String? = null,
        attachments: List<FilePartInput> = emptyList(),
        model: ModelRef? = null,
        agent: String? = null,
    ) = transport.post(
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
        transport.get("/command", directory = directory) { json ->
            val arr = json as? JsonArray ?: return@get emptyList()
            arr.mapNotNull {
                try { Opcode42Json.decodeFromJsonElement(CommandInfo.serializer(), it) } catch (_: Exception) { null }
            }
        }

    /** GET /provider — providers and their models, for the model picker. */
    suspend fun listProviders(directory: String? = null): ProvidersResponse =
        transport.get("/provider", directory = directory) { json ->
            try {
                Opcode42Json.decodeFromJsonElement(ProvidersResponse.serializer(), json)
            } catch (_: Exception) {
                ProvidersResponse()
            }
        }

    /** GET /agent — selectable agents (modes), for the agent picker. */
    suspend fun listAgents(directory: String? = null): List<AgentInfo> =
        transport.get("/agent", directory = directory) { json ->
            val arr = json as? JsonArray ?: return@get emptyList()
            arr.mapNotNull {
                try { Opcode42Json.decodeFromJsonElement(AgentInfo.serializer(), it) } catch (_: Exception) { null }
            }
        }

    /** GET /find/file?query= — fuzzy file paths for @-mentions. */
    suspend fun findFiles(query: String, directory: String? = null, limit: Int = 20): List<String> {
        val base = transport.requireBaseUrl()
        val params = buildString {
            append("?query=").append(HttpTransport.enc(query))
            append("&limit=").append(limit)
            directory?.let { append("&directory=").append(HttpTransport.enc(it)) }
        }
        val req = Request.Builder().url("$base/find/file$params").also {
            directory?.let { d -> it.header("X-Opencode-Directory", HttpTransport.enc(d)) }
        }.get().build()
        return transport.execute(req) { json ->
            (json as? JsonArray)?.mapNotNull { it.jsonPrimitive.contentOrNull } ?: emptyList()
        }
    }

    /** POST /session/{id}/command — run a slash command with arguments. */
    suspend fun runCommand(
        sessionId: String,
        command: String,
        arguments: String,
        directory: String? = null,
    ) = transport.post(
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
    suspend fun createPty(directory: String): PtyInfo = transport.post(
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
     * `ws://host/pty/{ptyId}/connect?auth_token=<base64>[&cursor=<n>]`
     *
     * The auth_token is base64(user:pass) or base64(:token). [cursor] resumes the
     * stream from an absolute UTF-16 code-unit offset (`-1` = current end, `0` =
     * full replay — the server default); pass the last cursor seen on reconnect to
     * avoid re-replaying the whole buffer (`internal/server/pty_ws.go` parseCursor).
     */
    fun connectPty(ptyId: String, authToken: String, cursor: Long? = null): PtyClient {
        val base = transport.requireBaseUrl()
        // Replace http(s):// with ws(s)://
        val wsBase = base
            .replace(Regex("^https://"), "wss://")
            .replace(Regex("^http://"), "ws://")
        val url = buildString {
            append("$wsBase/pty/$ptyId/connect?auth_token=${HttpTransport.enc(authToken)}")
            if (cursor != null) append("&cursor=$cursor")
        }
        val output = Channel<String>(Channel.UNLIMITED)
        val cursorCh = Channel<Long>(Channel.CONFLATED)
        val listener = PtyClient.createListener(output, cursorCh)
        val request = Request.Builder().url(url).build()
        val ws = transport.webSocket(request, listener)
        return PtyClient(ws, output, cursorCh)
    }

    /**
     * Resizes the PTY's terminal window. `PUT /pty/{ptyId}` with `{"size":{"rows","cols"}}`
     * (`internal/pty/pty.go` UpdateInput / Size, mirrors opencode `pty/index.ts:80-90`).
     * Best-effort: the keyboard-driven mobile terminal sends this on layout changes.
     */
    suspend fun resizePty(ptyId: String, rows: Int, cols: Int) = transport.put(
        path = "/pty/$ptyId",
        body = buildJsonObject {
            put("size", buildJsonObject {
                put("rows", rows)
                put("cols", cols)
            })
        },
    ) { _ -> Unit }

    // ─── Diff ─────────────────────────────────────────────────────────────────

    suspend fun getSessionDiff(
        sessionId: String,
        messageId: String,
        directory: String,
    ): List<SnapshotFileDiff> {
        val base = transport.requireBaseUrl()
        val url = "$base/session/$sessionId/diff" +
            "?messageID=${HttpTransport.enc(messageId)}" +
            "&directory=${HttpTransport.enc(directory)}"
        val req = Request.Builder().url(url)
            .header("X-Opencode-Directory", HttpTransport.enc(directory))
            .get()
            .build()
        return transport.execute(req) { json ->
            (json as? JsonArray)?.map { Opcode42Json.decodeFromJsonElement(SnapshotFileDiff.serializer(), it) }
                ?: emptyList()
        }
    }

    // ─── Permission ───────────────────────────────────────────────────────────

    suspend fun replyPermission(requestId: String, allow: Boolean) =
        transport.post(
            path = "/permission/$requestId/reply",
            body = buildJsonObject { put("allow", allow) },
        ) { _ -> Unit }

    // ─── Question ─────────────────────────────────────────────────────────────

    suspend fun replyQuestion(requestId: String, answer: String) =
        transport.post(
            path = "/question/$requestId/reply",
            body = buildJsonObject { put("answer", answer) },
        ) { _ -> Unit }

    suspend fun rejectQuestion(requestId: String) =
        transport.post(
            path = "/question/$requestId/reject",
            body = JsonObject(emptyMap()),
        ) { _ -> Unit }

    // ─── Session fork / delete ────────────────────────────────────────────────

    suspend fun forkSession(sessionId: String): Session = transport.post(
        path = "/session/$sessionId/fork",
        body = JsonObject(emptyMap()),
    ) { json -> Opcode42Json.decodeFromJsonElement(Session.serializer(), json) }

    suspend fun deleteSession(sessionId: String) = transport.delete("/session/$sessionId")

    /** POST /session/{id}/abort — interrupt a running agent turn. Returns true if it was aborted. */
    suspend fun abortSession(sessionId: String, directory: String? = null): Boolean = transport.post(
        path = "/session/$sessionId/abort",
        body = JsonObject(emptyMap()),
        directory = directory,
    ) { json -> (json as? JsonPrimitive)?.booleanOrNull ?: false }

    /** PATCH /session/{id} — rename (set the title). Returns the updated session. */
    suspend fun renameSession(sessionId: String, title: String, directory: String? = null): Session = transport.patch(
        path = "/session/$sessionId",
        body = buildJsonObject { put("title", title) },
        directory = directory,
    ) { json -> Opcode42Json.decodeFromJsonElement(Session.serializer(), json) }

    /**
     * PATCH /session/{id} — archive the session by setting `time.archived` to a
     * finite Unix-ms timestamp. opencode has NO un-archive path: archived is set-only
     * (a JSON null/absent for archived is a no-op), so we only ever set it
     * (session.ts:731-732, groups/session.ts:46-54). Returns the updated session.
     */
    suspend fun archiveSession(
        sessionId: String,
        archivedAt: Long = System.currentTimeMillis(),
        directory: String? = null,
    ): Session = transport.patch(
        path = "/session/$sessionId",
        body = buildJsonObject {
            put("time", buildJsonObject { put("archived", archivedAt) })
        },
        directory = directory,
    ) { json -> Opcode42Json.decodeFromJsonElement(Session.serializer(), json) }

    /** POST /session/{id}/summarize — compact the context using the given model. */
    suspend fun summarizeSession(sessionId: String, model: ModelRef, directory: String? = null): Boolean = transport.post(
        path = "/session/$sessionId/summarize",
        body = buildJsonObject {
            put("providerID", model.providerID)
            put("modelID", model.modelID)
        },
        directory = directory,
    ) { json -> (json as? JsonPrimitive)?.booleanOrNull ?: false }

    /** POST /session/{id}/share — publish a shareable link. Returns the session with share.url. */
    suspend fun shareSession(sessionId: String, directory: String? = null): Session = transport.post(
        path = "/session/$sessionId/share",
        body = JsonObject(emptyMap()),
        directory = directory,
    ) { json -> Opcode42Json.decodeFromJsonElement(Session.serializer(), json) }

    /** DELETE /session/{id}/share — revoke the shared link. Returns the updated session. */
    suspend fun unshareSession(sessionId: String, directory: String? = null): Session = transport.delete(
        path = "/session/$sessionId/share",
        directory = directory,
    ) { json -> Opcode42Json.decodeFromJsonElement(Session.serializer(), json) }

    // ─── Push notifications (plan 13 §13.8) ─────────────────────────────────────

    /**
     * POST /push/register — register or refresh this device's FCM token so the
     * daemon's relay can push `session.idle` / `permission.asked` /
     * `question.asked` notifications when no SSE client is connected. Re-calling
     * with the same [deviceId] refreshes the token (the daemon upserts by
     * device_id), which is the path taken on FCM token rotation.
     *
     * Opcode42 known-addition (opencode has no push surface); the body matches
     * `internal/server/push_handlers.go` pushRegisterInput. Returns true on success.
     */
    suspend fun registerPush(
        deviceId: String,
        fcmToken: String,
        platform: String = "android",
        sessionFilter: List<String>? = null,
    ): Boolean = transport.post(
        path = "/push/register",
        body = buildJsonObject {
            put("device_id", deviceId)
            put("fcm_token", fcmToken)
            put("platform", platform)
            sessionFilter?.let { filter ->
                put("session_filter", buildJsonArray { filter.forEach { add(it) } })
            }
        },
    ) { json -> (json as? JsonPrimitive)?.booleanOrNull ?: true }

    /**
     * DELETE /push/register/{deviceID} — unregister this device (logout/teardown)
     * so the daemon stops fanning push out to a token we no longer own. A 404
     * (device not registered) is treated as success — the desired end state
     * (no registration) already holds.
     */
    suspend fun unregisterPush(deviceId: String): Boolean {
        val req = Request.Builder()
            .url(transport.buildUrl("/push/register/${HttpTransport.enc(deviceId)}", null))
            .delete()
            .build()
        return transport.call(req) { resp ->
            if (resp.isSuccessful || resp.code == 404) true
            else throw HttpException(resp.code, resp.body?.string())
        }
    }
}
