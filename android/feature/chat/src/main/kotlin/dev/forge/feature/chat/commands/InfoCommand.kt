package dev.forge.feature.chat.commands

/** `/info` — show session info: tokens, cost, model, changes. Hidden when the info
 *  panel is already visible inline (expanded layout). */
object InfoCommand : BuiltinCommand {
    override val name = "info"
    override val description = "Session info and usage"
    override fun isAvailable(actions: ChatCommandActions) = !actions.infoPanelVisible
    override fun execute(actions: ChatCommandActions) = actions.openInfo()
}
