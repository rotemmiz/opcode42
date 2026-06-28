package dev.forge.feature.chat.commands

/** `/share` — publish or manage the session's shared link. */
object ShareCommand : BuiltinCommand {
    override val name = "share"
    override val description = "Share session"
    override fun execute(actions: ChatCommandActions) = actions.shareSession()
}
