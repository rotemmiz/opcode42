package dev.forge.feature.chat.commands

/** `/delete` — delete the current session (asks for confirmation first). */
object DeleteCommand : BuiltinCommand {
    override val name = "delete"
    override val description = "Delete session"
    override fun execute(actions: ChatCommandActions) = actions.deleteSession()
}
