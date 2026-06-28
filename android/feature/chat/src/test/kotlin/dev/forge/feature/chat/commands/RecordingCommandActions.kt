package dev.forge.feature.chat.commands

/** Test double that records which capability a command invoked. */
class RecordingCommandActions(
    override val hasDirectory: Boolean = true,
) : ChatCommandActions {
    val calls = mutableListOf<String>()

    override fun newSession() { calls += "newSession" }
    override fun openSessions() { calls += "openSessions" }
    override fun openModelPicker() { calls += "openModelPicker" }
    override fun openTerminal() { calls += "openTerminal" }
    override fun toggleTheme() { calls += "toggleTheme" }
    override fun openInfo() { calls += "openInfo" }
    override fun renameSession() { calls += "renameSession" }
    override fun forkSession() { calls += "forkSession" }
    override fun summarize() { calls += "summarize" }
    override fun shareSession() { calls += "shareSession" }
    override fun archiveSession() { calls += "archiveSession" }
    override fun deleteSession() { calls += "deleteSession" }
}
