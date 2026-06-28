package dev.forge.feature.chat.commands

/** `/models` — switch the model for upcoming prompts (opens the model + agent picker). */
object ModelsCommand : BuiltinCommand {
    override val name = "models"
    override val description = "Switch model"
    override fun execute(actions: ChatCommandActions) = actions.openModelPicker()
}
