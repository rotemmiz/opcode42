package dev.opcode42.feature.chat.commands

/** `/fork` — branch the current session into a new one. */
object ForkCommand : BuiltinCommand {
    override val name = "fork"
    override val description = "Fork session"
    override fun execute(actions: ChatCommandActions) = actions.forkSession()
}
