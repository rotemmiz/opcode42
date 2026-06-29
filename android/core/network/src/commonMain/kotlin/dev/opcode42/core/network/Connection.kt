package dev.opcode42.core.network

import kotlinx.coroutines.flow.StateFlow

/**
 * Lightweight contract so networking code (e.g. AuthInterceptor on Android)
 * doesn't depend on the full ServerConnectionManager.
 *
 * Pure multiplatform types (commonMain) — shareable with a future iOS client.
 */
interface ActiveConnectionProvider {
    val active: ServerConnectionConfig?
    val activeFlow: StateFlow<ServerConnectionConfig?>
}

data class ServerConnectionConfig(
    val url: String,
    val http: HttpConfig,
    val directory: String? = null,
)

data class HttpConfig(
    val url: String,
    val username: String? = null,
    val password: String? = null,
)
