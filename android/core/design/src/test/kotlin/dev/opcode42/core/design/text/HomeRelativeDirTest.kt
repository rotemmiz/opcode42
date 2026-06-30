package dev.opcode42.core.design.text

import org.junit.Assert.assertEquals
import org.junit.Test

class HomeRelativeDirTest {

    @Test fun `collapses macOS, linux, and root homes`() {
        assertEquals("~/git/opcode42", homeRelativeDir("/Users/rotemmiz/git/opcode42"))
        assertEquals("~/src/app", homeRelativeDir("/home/dev/src/app"))
        assertEquals("~/x", homeRelativeDir("/home/bob/x"))
        assertEquals("~/work", homeRelativeDir("/root/work"))
        assertEquals("~/x", homeRelativeDir("/root/x"))
    }

    @Test fun `bare home root collapses to tilde`() {
        assertEquals("~", homeRelativeDir("/Users/rotemmiz"))
        assertEquals("~", homeRelativeDir("/root"))
    }

    @Test fun `only whole segments match`() {
        // No slash after the home root → not a home prefix.
        assertEquals("/homestead/app", homeRelativeDir("/homestead/app"))
        assertEquals("/rooted/app", homeRelativeDir("/rooted/app"))
        // Regression: the old `Regex("^/root")` lacked a boundary and produced "~kit/x".
        assertEquals("/rootkit/x", homeRelativeDir("/rootkit/x"))
    }

    @Test fun `non-home and partial paths are unchanged`() {
        assertEquals("/var/log", homeRelativeDir("/var/log"))
        assertEquals("/var/lib/opcode", homeRelativeDir("/var/lib/opcode"))
        assertEquals("/Users", homeRelativeDir("/Users")) // bare root segment, no user
    }

    @Test fun `only the leading prefix is collapsed`() {
        assertEquals("~/backups/Users/bob", homeRelativeDir("/Users/alice/backups/Users/bob"))
    }

    @Test fun `blank or null yields empty`() {
        assertEquals("", homeRelativeDir(null))
        assertEquals("", homeRelativeDir(""))
        assertEquals("", homeRelativeDir("   "))
    }
}
