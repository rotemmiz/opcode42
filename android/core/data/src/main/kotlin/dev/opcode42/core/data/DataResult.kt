package dev.opcode42.core.data

import dev.opcode42.core.sdk.HttpException
import dev.opcode42.core.sdk.NotConfiguredException
import kotlinx.coroutines.CancellationException
import java.io.IOException

/**
 * Runs [block] and wraps the outcome in a [Result]. Unlike `runCatching`, this **rethrows**
 * [CancellationException] so structured-concurrency cancellation is never swallowed — the
 * repository boundary turns expected failures into `Result.failure`, not coroutine cancellation.
 */
internal suspend fun <T> resultOf(block: suspend () -> T): Result<T> =
    try {
        Result.success(block())
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        Result.failure(e)
    }

/** Maps a repository failure to a short, user-facing message (for snackbars in the UI layer). */
fun Throwable.toUserMessage(): String = when (this) {
    is NotConfiguredException -> "No server configured"
    is HttpException -> when (code) {
        401, 403 -> "Not authorized"
        404 -> "Not found"
        in 500..599 -> "Server error ($code)"
        else -> "Request failed ($code)"
    }
    is IOException -> "Can't reach the server"
    else -> "Something went wrong"
}

/**
 * True when the failure is HTTP 404 — the request was already answered/cancelled elsewhere
 * (the optimistic clear already removed it from the store, so the UI is already correct).
 * Used by the permission/question reply `onFailure` handlers to swallow a stale-tap 404
 * silently instead of surfacing a misleading "Not found" snackbar.
 */
fun Throwable.isNotFound(): Boolean = this is HttpException && code == 404
