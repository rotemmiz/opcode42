package dev.forge.feature.chat.commands

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class BuiltinCommandsTest {

    private val comingSoon = setOf("diff", "timeline", "variant", "stash")

    @Test
    fun registryHasNoDuplicateNames() {
        val names = builtinCommands.map { it.name }
        assertEquals(names.size, names.toSet().size)
    }

    @Test
    fun registryExposesExpectedActions() {
        val names = builtinCommands.map { it.name }.toSet()
        val expected = setOf(
            "new", "sessions", "models", "agents", "terminal", "theme", "info",
            "rename", "fork", "summarize", "share", "archive", "delete",
            "diff", "timeline", "variant", "stash",
        )
        assertEquals(expected, names)
    }

    @Test
    fun comingSoonCommandsAreUnimplementedAndRestImplemented() {
        for (cmd in builtinCommands) {
            if (cmd.name in comingSoon) {
                assertFalse("${cmd.name} should be unimplemented", cmd.implemented)
            } else {
                assertTrue("${cmd.name} should be implemented", cmd.implemented)
            }
        }
    }
}
