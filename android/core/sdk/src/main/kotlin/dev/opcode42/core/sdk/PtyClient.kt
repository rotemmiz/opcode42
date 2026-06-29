package dev.opcode42.core.sdk

import android.util.Log
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.receiveAsFlow
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString

/**
 * Wraps an OkHttp WebSocket for PTY I/O, matching the Opcode42 daemon's PTY wire
 * contract (`internal/server/pty_ws.go`, mirrors opencode `pty/index.ts`):
 *
 *   - **Text frames** carry raw UTF-8 terminal output (the daemon writes data as
 *     `MessageText`). These are the bytes the [TerminalEmulator] interprets.
 *   - **Binary frames** carry the control frame: a leading `0x00` byte followed by
 *     UTF-8 JSON `{"cursor":<n>}`. The cursor is the absolute count of UTF-16 code
 *     units the server has ever emitted; clients track it so a reconnect can resume
 *     with `?cursor=<n>` and avoid replaying the whole buffer.
 *
 * Output is delivered as decoded [String] chunks on [output]; the consumer feeds
 * them into a [TerminalEmulator]. Cursor updates are delivered on [cursor].
 *
 * Outgoing: raw keystroke bytes sent as binary frames → PTY stdin.
 *
 * Uses unlimited [Channel]s for backpressure — no frames are ever dropped. Channel
 * close (via [close] or on WebSocket failure/closure) signals EOF.
 */
class PtyClient(
    private val webSocket: WebSocket,
    private val outputChannel: Channel<String> = Channel(Channel.UNLIMITED),
    private val cursorChannel: Channel<Long> = Channel(Channel.CONFLATED),
) {
    /** Decoded terminal-output chunks, in arrival order. */
    val output: Flow<String> = outputChannel.receiveAsFlow()

    /** Latest server cursor (absolute UTF-16 code-unit count) from control frames. */
    val cursor: Flow<Long> = cursorChannel.receiveAsFlow()

    fun send(bytes: ByteArray) {
        webSocket.send(ByteString.of(*bytes))
    }

    fun close() {
        webSocket.close(1000, null)
        outputChannel.close()
        cursorChannel.close()
    }

    companion object {
        /** Control frames are tagged with a leading NUL byte. */
        private const val CONTROL_PREFIX = 0x00.toByte()
        private val CURSOR_REGEX = Regex("\"cursor\"\\s*:\\s*(\\d+)")

        /**
         * Creates a [WebSocketListener] that fans frames out to the given channels.
         * Create the listener, build the WebSocket, then construct
         * `PtyClient(ws, output, cursor)`.
         */
        fun createListener(
            output: Channel<String>,
            cursor: Channel<Long>,
        ): WebSocketListener =
            object : WebSocketListener() {
                // Text frames = terminal output (the daemon's data path).
                override fun onMessage(webSocket: WebSocket, text: String) {
                    if (text.isNotEmpty()) output.trySend(text)
                }

                // Binary frames = control frames (0x00 + JSON {cursor}).
                override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                    val raw = bytes.toByteArray()
                    if (raw.isEmpty()) return
                    if (raw[0] == CONTROL_PREFIX) {
                        parseCursor(raw)?.let { cursor.trySend(it) }
                        return
                    }
                    // Defensive: some intermediaries may relay output as binary.
                    output.trySend(String(raw, Charsets.UTF_8))
                }

                override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                    Log.e("PtyClient", "WebSocket failure", t)
                    output.close()
                    cursor.close()
                }

                override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                    output.close()
                    cursor.close()
                }
            }

        /** Extracts the `cursor` field from a `0x00 + JSON` control frame, or null. */
        internal fun parseCursor(frame: ByteArray): Long? {
            if (frame.isEmpty() || frame[0] != CONTROL_PREFIX) return null
            val json = String(frame, 1, frame.size - 1, Charsets.UTF_8)
            return CURSOR_REGEX.find(json)?.groupValues?.get(1)?.toLongOrNull()
        }
    }
}
