package dev.opcode42.feature.chat.commands

import dev.opcode42.core.model.CommandInfo
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PaletteEntryTest {

    private fun daemon(name: String, description: String? = null, source: String? = null) =
        CommandInfo(name = name, description = description, source = source)

    @Test
    fun builtinsComeBeforeDaemonCommands() {
        val entries = buildPaletteEntries(
            builtinCommands,
            listOf(daemon("deploy")),
            RecordingCommandActions(),
        )
        val lastBuiltin = entries.indexOfLast { it is PaletteEntry.Builtin }
        val firstDaemon = entries.indexOfFirst { it is PaletteEntry.Daemon }
        assertTrue(lastBuiltin < firstDaemon)
    }

    @Test
    fun comingSoonEntriesAreDisabledWithSoonBadge() {
        val entries = buildPaletteEntries(builtinCommands, emptyList(), RecordingCommandActions())
        val diff = entries.first { it.name == "diff" }
        assertFalse(diff.enabled)
        assertEquals("soon", diff.badge)
    }

    @Test
    fun terminalHiddenWithoutDirectoryAndShownWithOne() {
        val without = buildPaletteEntries(
            builtinCommands, emptyList(), RecordingCommandActions(hasDirectory = false),
        )
        assertNull(without.firstOrNull { it.name == "terminal" })

        val with = buildPaletteEntries(
            builtinCommands, emptyList(), RecordingCommandActions(hasDirectory = true),
        )
        assertNotNull(with.firstOrNull { it.name == "terminal" })
    }

    @Test
    fun daemonSourceMapsToBadge() {
        val entries = buildPaletteEntries(
            emptyList(),
            listOf(daemon("a", source = "mcp"), daemon("b", source = "skill"), daemon("c", source = "command")),
            RecordingCommandActions(),
        )
        assertEquals("mcp", entries.first { it.name == "a" }.badge)
        assertEquals("skill", entries.first { it.name == "b" }.badge)
        assertNull(entries.first { it.name == "c" }.badge)
    }

    @Test
    fun emptyDaemonNamesAreDropped() {
        val entries = buildPaletteEntries(emptyList(), listOf(daemon("")), RecordingCommandActions())
        assertTrue(entries.isEmpty())
    }

    @Test
    fun keysAreUniqueEvenWhenBuiltinAndDaemonShareAName() {
        // A user-defined `/diff` collides with the built-in `diff` by name; keys must still differ
        // so LazyColumn doesn't throw on duplicate keys.
        val entries = buildPaletteEntries(
            builtinCommands,
            listOf(daemon("diff"), daemon("deploy")),
            RecordingCommandActions(),
        )
        val keys = entries.map { it.key }
        assertEquals(keys.size, keys.toSet().size)
        // Both the built-in and the daemon `diff` are present (TUI-style: show both).
        assertEquals(2, entries.count { it.name == "diff" })
    }

    @Test
    fun duplicateDaemonNamesAreDeduped() {
        val entries = buildPaletteEntries(
            emptyList(),
            listOf(daemon("deploy", source = "command"), daemon("deploy", source = "mcp")),
            RecordingCommandActions(),
        )
        assertEquals(1, entries.size)
    }

    @Test
    fun filterByQueryIsCaseInsensitive() {
        val entries = buildPaletteEntries(builtinCommands, emptyList(), RecordingCommandActions())
        val result = entries.filterByQuery("MO")
        assertTrue(result.any { it.name == "models" })
        assertTrue(result.all { it.name.contains("mo", ignoreCase = true) })
    }

    @Test
    fun emptyQueryReturnsEverything() {
        val entries = buildPaletteEntries(builtinCommands, emptyList(), RecordingCommandActions())
        assertEquals(entries, entries.filterByQuery(""))
    }
}
