package dev.forge.feature.connections

import dev.forge.core.network.HttpConfig
import kotlinx.serialization.Serializable

/**
 * Mirrors opencode's ServerConnection.Http from
 * packages/app/src/context/server.tsx:71-83.
 */
sealed class ServerConnection {
    abstract val http: HttpConfig
    abstract val displayName: String?

    /** Stable identifier for deduplication — uses the normalized URL. */
    fun key(): String = when (this) {
        is Http -> http.url
    }

    data class Http(
        override val http: HttpConfig,
        override val displayName: String? = null,
        val directory: String? = null,
    ) : ServerConnection()
}

@Serializable
data class PersistedConnection(
    val url: String,
    val username: String? = null,
    val password: String? = null,
    val displayName: String? = null,
    val directory: String? = null,
)

fun PersistedConnection.toServerConnection() = ServerConnection.Http(
    http = HttpConfig(url, username, password),
    displayName = displayName,
    directory = directory,
)

fun ServerConnection.Http.toPersisted() = PersistedConnection(
    url = http.url,
    username = http.username,
    password = http.password,
    displayName = displayName,
    directory = directory,
)

/**
 * Mirrors normalizeServerUrl from packages/app/src/context/server.tsx:10-15.
 * - Strips trailing slash
 * - Adds http:// prefix if no scheme present
 */
fun normalizeServerUrl(raw: String): String {
    val trimmed = raw.trim().trimEnd('/')
    return if (trimmed.startsWith("http://") || trimmed.startsWith("https://")) trimmed
    else "http://$trimmed"
}
