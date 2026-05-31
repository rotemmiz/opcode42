package dev.forge.core.network

import okhttp3.Credentials
import okhttp3.Interceptor
import okhttp3.Response
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Adds Authorization: Basic header to all requests when credentials are configured.
 * Mirrors opencode's authorization.ts:84 — Basic scheme, base64(user:pass).
 */
@Singleton
class AuthInterceptor @Inject constructor(
    private val connectionProvider: ActiveConnectionProvider,
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val conn = connectionProvider.active ?: return chain.proceed(chain.request())
        val cfg = conn.http
        val req = if (cfg.username != null && cfg.password != null) {
            chain.request().newBuilder()
                .header("Authorization", Credentials.basic(cfg.username, cfg.password))
                .build()
        } else {
            chain.request()
        }
        return chain.proceed(req)
    }
}

/** Lightweight interface so AuthInterceptor doesn't depend on the full ServerConnectionManager. */
interface ActiveConnectionProvider {
    val active: ServerConnectionConfig?
}

data class ServerConnectionConfig(
    val url: String,
    val http: HttpConfig,
)

data class HttpConfig(
    val url: String,
    val username: String? = null,
    val password: String? = null,
)
