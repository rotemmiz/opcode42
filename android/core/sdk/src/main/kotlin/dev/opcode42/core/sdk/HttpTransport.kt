package dev.opcode42.core.sdk

import dev.opcode42.core.model.Opcode42Json
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import java.io.IOException
import java.net.URLEncoder
import javax.inject.Inject
import javax.inject.Named
import javax.inject.Singleton

private val JSON_MEDIA = "application/json; charset=utf-8".toMediaType()

/** Non-2xx HTTP response. [body] is the (possibly truncated/raw) response body for logging. */
class HttpException(val code: Int, val body: String?) : IOException("HTTP $code")

/** No server has been configured/selected yet (no base URL). */
class NotConfiguredException : IllegalStateException("No server configured")

/** Source of the active daemon base URL (bound by :feature:connections). */
interface BaseUrlProvider {
    val baseUrl: String?
}

/**
 * The transport seam: owns OkHttp, URL building, the `X-Opencode-Directory` routing header,
 * IO dispatch, and non-2xx → [HttpException] mapping. Endpoint definitions live in
 * [Opcode42Client]; JSON→domain mapping lives in `Mappers.kt`.
 *
 * GET requests carry the directory in BOTH the `?directory=` query and the header; writes
 * carry it in the header only — this asymmetry mirrors the prior hand-rolled client and the
 * daemon's routing (plan 06), so it is preserved deliberately.
 */
@Singleton
class HttpTransport @Inject constructor(
    private val httpClient: OkHttpClient,
    // Long-lived PTY WebSocket must not inherit the REST client's finite read timeout, or an
    // idle shell would drop after ~30s. Name must match NetworkModule.STREAMING_CLIENT.
    @Named("streaming") private val streamingClient: OkHttpClient,
    private val baseUrlProvider: BaseUrlProvider,
) {
    val baseUrl: String? get() = baseUrlProvider.baseUrl

    fun requireBaseUrl(): String = baseUrl?.trimEnd('/') ?: throw NotConfiguredException()

    fun buildUrl(path: String, directory: String?): String {
        val base = requireBaseUrl()
        return if (directory != null) "$base$path?directory=${enc(directory)}" else "$base$path"
    }

    fun webSocket(request: Request, listener: WebSocketListener): WebSocket =
        streamingClient.newWebSocket(request, listener)

    // ─── Core call/execute ──────────────────────────────────────────────────────

    /** Runs [request] on IO and hands the raw [Response] to [block] (no status check). */
    suspend fun <T> call(request: Request, block: (Response) -> T): T = withContext(Dispatchers.IO) {
        httpClient.newCall(request).execute().use(block)
    }

    /**
     * Runs [request], throws [HttpException] on non-2xx, then parses the body via [parse].
     * [tolerant] (write responses) falls back to an empty object when the body is absent or
     * not valid JSON; strict (reads) lets a parse failure propagate.
     */
    suspend fun <T> execute(
        request: Request,
        tolerant: Boolean = false,
        parse: (JsonElement) -> T,
    ): T = call(request) { resp ->
        val body = resp.body?.string() ?: "{}"
        if (!resp.isSuccessful) throw HttpException(resp.code, body)
        val elem = if (tolerant) {
            runCatching { Opcode42Json.parseToJsonElement(body) }.getOrDefault(JsonObject(emptyMap()))
        } else {
            Opcode42Json.parseToJsonElement(body)
        }
        parse(elem)
    }

    // ─── Verb helpers ─────────────────────────────────────────────────────────────

    suspend fun <T> get(path: String, directory: String? = null, parse: (JsonElement) -> T): T =
        execute(
            Request.Builder().url(buildUrl(path, directory)).get()
                .withDirectory(directory).build(),
            parse = parse,
        )

    suspend fun <T> post(
        path: String,
        body: JsonObject,
        directory: String? = null,
        parse: (JsonElement) -> T,
    ): T = execute(
        Request.Builder().url(buildUrl(path, null))
            .post(body.toString().toRequestBody(JSON_MEDIA))
            .withDirectory(directory).build(),
        tolerant = true,
        parse = parse,
    )

    suspend fun <T> patch(
        path: String,
        body: JsonObject,
        directory: String? = null,
        parse: (JsonElement) -> T,
    ): T = execute(
        Request.Builder().url(buildUrl(path, null))
            .patch(body.toString().toRequestBody(JSON_MEDIA))
            .withDirectory(directory).build(),
        tolerant = true,
        parse = parse,
    )

    suspend fun <T> put(
        path: String,
        body: JsonObject,
        directory: String? = null,
        parse: (JsonElement) -> T,
    ): T = execute(
        Request.Builder().url(buildUrl(path, null))
            .put(body.toString().toRequestBody(JSON_MEDIA))
            .withDirectory(directory).build(),
        tolerant = true,
        parse = parse,
    )

    /** DELETE that parses a response body (e.g. `/share` returns the updated session). */
    suspend fun <T> delete(path: String, directory: String? = null, parse: (JsonElement) -> T): T =
        execute(
            Request.Builder().url(buildUrl(path, null)).delete()
                .withDirectory(directory).build(),
            tolerant = true,
            parse = parse,
        )

    /** DELETE with no response body; throws [HttpException] on non-2xx. */
    suspend fun delete(path: String) {
        val req = Request.Builder().url(buildUrl(path, null)).delete().build()
        call(req) { resp -> if (!resp.isSuccessful) throw HttpException(resp.code, resp.body?.string()) }
    }

    private fun Request.Builder.withDirectory(directory: String?): Request.Builder = also {
        directory?.let { d -> it.header("X-Opencode-Directory", enc(d)) }
    }

    companion object {
        /**
         * Encodes a path/query/header component with `encodeURIComponent` semantics (UTF-8).
         *
         * `URLEncoder.encode` emits space as `+`, but the daemon decodes the
         * `X-Opencode-Directory` header with Go's `url.PathUnescape`, which decodes `%xx` yet
         * leaves `+` literal (server/directory.go) — so a directory path containing a space would
         * route to the wrong instance (`/a b` → `/a+b`). Emitting `%20` instead round-trips
         * through both `PathUnescape` (the header) and `QueryUnescape` (the `?directory=` query).
         */
        fun enc(value: String): String = URLEncoder.encode(value, "UTF-8").replace("+", "%20")
    }
}
