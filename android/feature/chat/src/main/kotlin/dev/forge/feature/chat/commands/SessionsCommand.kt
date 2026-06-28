package dev.forge.feature.chat.commands

/** `/sessions` — switch session (nav rail in multi-pane, session list in compact). */
object SessionsCommand : BuiltinCommand {
    override val name = "sessions"
    override val description = "Switch session"
    override fun execute(actions: ChatCommandActions) = actions.openSessions()
}
