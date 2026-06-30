package dev.opcode42.feature.sessions

import org.junit.Assert.assertEquals
import org.junit.Test

/** Coverage for the daemon-host directory → `~`-relative display helper. */
class SessionPathTest {

    @Test fun homeRelativeDir_macAndLinuxHomes() {
        assertEquals("~/git/opcode42", homeRelativeDir("/Users/rotemmiz/git/opcode42"))
        assertEquals("~", homeRelativeDir("/Users/bob"))
        assertEquals("~/x", homeRelativeDir("/home/bob/x"))
        assertEquals("~/x", homeRelativeDir("/root/x"))
        assertEquals("~", homeRelativeDir("/root"))
    }

    @Test fun homeRelativeDir_nonHomePathsUnchanged() {
        assertEquals("/var/log", homeRelativeDir("/var/log"))
        // A bare `/Users` with no user segment is not a home and is left alone.
        assertEquals("/Users", homeRelativeDir("/Users"))
    }

    @Test fun homeRelativeDir_blankOrNull() {
        assertEquals("", homeRelativeDir(null))
        assertEquals("", homeRelativeDir(""))
        assertEquals("", homeRelativeDir("   "))
    }
}
