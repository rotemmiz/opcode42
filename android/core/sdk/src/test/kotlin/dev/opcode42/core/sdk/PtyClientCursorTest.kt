package dev.opcode42.core.sdk

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Pins the PTY control-frame contract (`internal/server/pty_ws.go` meta(): a
 * binary frame of `0x00` + UTF-8 JSON `{"cursor":n}`). The client parses the
 * cursor so a reconnect can resume with `?cursor=<n>`.
 */
class PtyClientCursorTest {

    private fun frame(json: String): ByteArray =
        byteArrayOf(0x00) + json.toByteArray(Charsets.UTF_8)

    @Test
    fun parsesCursorFromControlFrame() {
        assertEquals(42L, PtyClient.parseCursor(frame("""{"cursor":42}""")))
    }

    @Test
    fun parsesCursorWithWhitespace() {
        assertEquals(7L, PtyClient.parseCursor(frame("""{ "cursor" : 7 }""")))
    }

    @Test
    fun parsesLargeCursor() {
        assertEquals(9_000_000_000L, PtyClient.parseCursor(frame("""{"cursor":9000000000}""")))
    }

    @Test
    fun returnsNullForNonControlFrame() {
        // No 0x00 prefix → this is terminal output, not a control frame.
        assertNull(PtyClient.parseCursor("hello".toByteArray(Charsets.UTF_8)))
    }

    @Test
    fun returnsNullForEmptyFrame() {
        assertNull(PtyClient.parseCursor(ByteArray(0)))
    }

    @Test
    fun returnsNullWhenCursorFieldMissing() {
        assertNull(PtyClient.parseCursor(frame("""{"other":1}""")))
    }
}
