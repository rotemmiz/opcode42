package dev.forge.core.sdk

import android.util.Log
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.receiveAsFlow
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString

/**
 * Wraps an OkHttp WebSocket for PTY I/O.
 *
 * Incoming binary frames:
 *   - Starting with 0x00: cursor control JSON — skipped (not rendered yet)
 *   - Otherwise: raw terminal output bytes — emitted on [output]
 *
 * Outgoing: raw keystroke bytes sent as binary frames.
 *
 * Uses an unlimited [Channel] for backpressure — no frames are ever dropped.
 * Channel close (via [close] or on WebSocket failure/closure) signals EOF.
 */
class PtyClient(
    private val webSocket: WebSocket,
    private val _channel: Channel<ByteArray> = Channel(Channel.UNLIMITED),
) {
    val output: Flow<ByteArray> = _channel.receiveAsFlow()

    fun send(bytes: ByteArray) {
        webSocket.send(ByteString.of(*bytes))
    }

    fun close() {
        webSocket.close(1000, null)
        _channel.close()
    }

    companion object {
        /**
         * Creates a WebSocketListener that emits to the given channel.
         * Create the listener, build the WebSocket, then construct PtyClient(ws, channel).
         */
        fun createListener(channel: Channel<ByteArray>): WebSocketListener =
            object : WebSocketListener() {
                override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                    val raw = bytes.toByteArray()
                    if (raw.isEmpty()) return
                    // 0x00 prefix = cursor control frame — skip for now
                    if (raw[0] == 0x00.toByte()) return
                    channel.trySend(raw)
                }

                override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                    Log.e("PtyClient", "WebSocket failure", t)
                    // Close channel to signal EOF
                    channel.close()
                }

                override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                    channel.close()
                }
            }
    }
}
