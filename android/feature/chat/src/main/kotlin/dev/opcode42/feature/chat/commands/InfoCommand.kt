package dev.opcode42.feature.chat.commands

/** `/info` — show session info: tokens, cost, model, changes. */
object InfoCommand : BuiltinCommand {
    override val name = "info"
    override val description = "Session info and usage"
    override fun execute(actions: ChatCommandActions) = actions.openInfo()
}
