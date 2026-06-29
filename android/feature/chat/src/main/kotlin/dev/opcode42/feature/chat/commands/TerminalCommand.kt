package dev.opcode42.feature.chat.commands

/** `/terminal` — open the embedded terminal for the session directory. */
object TerminalCommand : BuiltinCommand {
    override val name = "terminal"
    override val description = "Open an embedded terminal"
    override fun isAvailable(actions: ChatCommandActions) = actions.hasDirectory
    override fun execute(actions: ChatCommandActions) = actions.openTerminal()
}
