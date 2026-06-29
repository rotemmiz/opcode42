package dev.opcode42.feature.chat.commands

/** `/new` — start a new session. */
object NewSessionCommand : BuiltinCommand {
    override val name = "new"
    override val description = "Start a new session"
    override fun execute(actions: ChatCommandActions) = actions.newSession()
}
