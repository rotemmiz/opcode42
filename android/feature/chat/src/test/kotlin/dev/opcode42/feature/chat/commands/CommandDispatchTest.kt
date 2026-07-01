package dev.opcode42.feature.chat.commands

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
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
}
