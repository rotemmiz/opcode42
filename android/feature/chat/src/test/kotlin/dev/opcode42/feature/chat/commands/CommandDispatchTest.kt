package dev.opcode42.feature.chat.commands

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CommandDispatchTest {

    @Test
    fun eachImplementedCommandInvokesExactlyItsCapability() {
        val expected = mapOf(
            NewSessionCommand to "newSession",
            SessionsCommand to "openSessions",
            ModelsCommand to "openModelPicker",
            AgentsCommand to "openAgentPicker",
            TerminalCommand to "openTerminal",
            InfoCommand to "openInfo",
            RenameCommand to "renameSession",
            ForkCommand to "forkSession",
            SummarizeCommand to "summarize",
            ShareCommand to "shareSession",
            ArchiveCommand to "archiveSession",
            DeleteCommand to "deleteSession",
        )
        for ((command, capability) in expected) {
            val rec = RecordingCommandActions()
            command.execute(rec)
            assertEquals("${command.name} dispatch", listOf(capability), rec.calls)
        }
    }

    @Test
    fun comingSoonCommandsDoNothing() {
        for (command in listOf(DiffCommand, TimelineCommand, VariantCommand, StashCommand)) {
            val rec = RecordingCommandActions()
            command.execute(rec)
            assertTrue("${command.name} should be a no-op", rec.calls.isEmpty())
            assertFalse(command.implemented)
        }
    }

    @Test
    fun parseSlashCommandSplitsNameAndArgsAtFirstSpace() {
        assertEquals(SlashCommandInput("cmd", ""), parseSlashCommand("/cmd"))
        assertEquals(SlashCommandInput("cmd", "foo bar"), parseSlashCommand("/cmd foo bar"))
        assertEquals(SlashCommandInput("cmd", "foo"), parseSlashCommand("/cmd foo"))
        assertEquals(SlashCommandInput("cmd", ""), parseSlashCommand("/cmd "))
        assertEquals(SlashCommandInput("", ""), parseSlashCommand("/"))
        assertEquals(SlashCommandInput("", ""), parseSlashCommand("/ "))
    }

    @Test
    fun parseSlashCommandReturnsNullForNonSlashOrMultilineText() {
        assertNull(parseSlashCommand(""))
        assertNull(parseSlashCommand("hello"))
        assertNull(parseSlashCommand("@mention"))
        assertNull(parseSlashCommand("/cmd\nfoo"))
        assertNull(parseSlashCommand(" /cmd"))
    }

    @Test
    fun parseSlashCommandPreservesArgsVerbatimIncludingExtraSpaces() {
        assertEquals(SlashCommandInput("cmd", "a  b\tc"), parseSlashCommand("/cmd a  b\tc"))
        assertEquals(SlashCommandInput("cmd", " foo bar "), parseSlashCommand("/cmd  foo bar "))
    }

    @Test
    fun typingSlashCommandWithArgsDispatchesNameAndArgs() {
        // Mirrors the PromptInput → ChatScreen wiring: parse the composer text,
        // filter the palette by the parsed command name, then pick the matched
        // daemon entry, forwarding the parsed args to runCommand.
        val text = "/cmd foo bar"
        val input = parseSlashCommand(text)
        assertNotNull(input)
        val palette = buildPaletteEntries(
            emptyList(),
            listOf(
                dev.opcode42.core.model.CommandInfo(name = "cmd"),
                dev.opcode42.core.model.CommandInfo(name = "other"),
            ),
            RecordingCommandActions(),
        )
        val matches = palette.filterByQuery(input!!.name)
        assertEquals(1, matches.size)
        val picked = matches.single { it.name == "cmd" }
        assertTrue(picked is PaletteEntry.Daemon)

        val dispatched = mutableListOf<Pair<String, String>>()
        fun runCommand(name: String, arguments: String) { dispatched += name to arguments }

        // ChatScreen's onPickEntry lambda:
        when (picked) {
            is PaletteEntry.Builtin -> picked.command.execute(RecordingCommandActions())
            is PaletteEntry.Daemon -> runCommand(picked.name, input.args)
        }

        assertEquals(listOf("cmd" to "foo bar"), dispatched)
    }
}
