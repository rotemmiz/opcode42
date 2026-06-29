package dev.opcode42.feature.chat.commands

/** `/rename` — rename the current session. */
object RenameCommand : BuiltinCommand {
    override val name = "rename"
    override val description = "Rename session"
    override fun execute(actions: ChatCommandActions) = actions.renameSession()
}
