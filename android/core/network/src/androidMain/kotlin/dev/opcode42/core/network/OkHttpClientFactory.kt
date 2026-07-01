package dev.opcode42.core.network

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import okhttp3.OkHttpClient
import okhttp3.logging.HttpLoggingInterceptor
import java.util.concurrent.TimeUnit
import javax.inject.Named
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object NetworkModule {
    // SseEventParser lives in commonMain (no DI annotations) so it can be shared
    // with a future iOS target; provide it as a Hilt @Singleton here.
    @Provides
    @Singleton
    fun provideSseEventParser(): SseEventParser = SseEventParser()

    /**
     * The default (REST) client. A finite read timeout is essential: without it a stalled or
     * half-open daemon connection hangs the calling coroutine forever. Long-lived streams use
     * [provideStreamingOkHttpClient] instead.
     */
    @Provides
    @Singleton
    fun provideOkHttpClient(authInterceptor: AuthInterceptor): OkHttpClient =
        OkHttpClient.Builder()
            .addInterceptor(authInterceptor)
            .addInterceptor(
                HttpLoggingInterceptor().apply {
                    level = HttpLoggingInterceptor.Level.BASIC
                }
            )
            .connectTimeout(30, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .writeTimeout(30, TimeUnit.SECONDS)
            .build()

    /**
     * Client for long-lived streams — the SSE event bus and the PTY WebSocket — which must not
     * time out mid-stream. Derived from the REST client so auth, logging, and the other timeouts
     * stay identical; only the read timeout is disabled.
     */
    @Provides
    @Singleton
    @Named(STREAMING_CLIENT)
    fun provideStreamingOkHttpClient(client: OkHttpClient): OkHttpClient =
        client.newBuilder()
            .readTimeout(0, TimeUnit.SECONDS)
            .build()

    const val STREAMING_CLIENT = "streaming"
}
