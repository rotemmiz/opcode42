package dev.opcode42.core.sdk

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Pins the `X-Opencode-Directory` / `?directory=` encoding contract. The daemon decodes the
 * routing header with Go's `url.PathUnescape`, which decodes `%xx` but leaves `+` literal — so a
 * space MUST be emitted as `%20`, not `+`, or a directory path with a space routes to the wrong
 * instance (see HttpTransport.enc).
 */
class HttpTransportEncTest {

    @Test
    fun enc_emitsPercent20ForSpace_notPlus() {
        assertEquals("%2Fa%20b", HttpTransport.enc("/a b"))
    }

    @Test
    fun enc_encodesSlashes() {
        assertEquals("%2FUsers%2Ffoo", HttpTransport.enc("/Users/foo"))
    }

    @Test
    fun enc_plainPathIsFullyReversibleByPathUnescape() {
        // Every escape is a %xx sequence (no bare '+'), so PathUnescape round-trips it exactly.
        assertEquals(false, HttpTransport.enc("/tmp/my project/sub dir").contains('+'))
    }
}
