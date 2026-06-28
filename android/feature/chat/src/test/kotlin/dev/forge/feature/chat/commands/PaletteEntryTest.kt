package dev.forge.feature.chat.commands

import dev.forge.core.model.CommandInfo
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
    fun orderingIsAvailableBuiltinsThenDaemonThenComingSoon() {
        val entries = buildPaletteEntries(
            builtinCommands,
            listOf(daemon("deploy")),
            RecordingCommandActions(),
        )
        val firstDaemon = entries.indexOfFirst { it is PaletteEntry.Daemon }
        val lastAvailableBuiltin = entries.indexOfLast { it is PaletteEntry.Builtin && it.enabled }
        val firstComingSoon = entries.indexOfFirst { it is PaletteEntry.Builtin && !it.enabled }
        // available built-ins → daemon → coming-soon
        assertTrue(lastAvailableBuiltin < firstDaemon)
        assertTrue(firstDaemon < firstComingSoon)
    }

    @Test
    fun availableBuiltinsKeepRegistryOrder() {
        val entries = buildPaletteEntries(builtinCommands, emptyList(), RecordingCommandActions())
        val newIdx = entries.indexOfFirst { it.name == "new" }
        val deleteIdx = entries.indexOfFirst { it.name == "delete" }
        assertTrue(newIdx in 0 until deleteIdx) // "new" precedes "delete" as in the registry
    }

    @Test
    fun infoHiddenWhenInfoPanelVisible() {
        val withPanel = buildPaletteEntries(
            builtinCommands, emptyList(), RecordingCommandActions(infoPanelVisible = true),
        )
        assertNull(withPanel.firstOrNull { it.name == "info" })

        val withoutPanel = buildPaletteEntries(
            builtinCommands, emptyList(), RecordingCommandActions(infoPanelVisible = false),
        )
        assertNotNull(withoutPanel.firstOrNull { it.name == "info" })
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

    @Test
    fun filterByQueryMatchesBothBuiltinAndDaemonEntries() {
        val entries = buildPaletteEntries(
            builtinCommands,
            listOf(daemon("shipit")),
            RecordingCommandActions(),
        )
        // "i" appears in built-ins (e.g. "info") and the daemon "shipit"
        val result = entries.filterByQuery("i")
        assertTrue(result.any { it is PaletteEntry.Builtin })
        assertTrue(result.any { it is PaletteEntry.Daemon })
    }

    @Test
    fun disabledBuiltinSurvivesFilter() {
        val entries = buildPaletteEntries(builtinCommands, emptyList(), RecordingCommandActions())
        val result = entries.filterByQuery("diff")
        val diff = result.first { it.name == "diff" }
        assertFalse(diff.enabled)
    }
}
