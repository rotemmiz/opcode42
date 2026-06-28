package dev.forge.feature.chat.commands

/** `/theme` — toggle between the light and dark theme. */
object ThemeCommand : BuiltinCommand {
    override val name = "theme"
    override val description = "Toggle light/dark theme"
    override fun execute(actions: ChatCommandActions) = actions.toggleTheme()
}
