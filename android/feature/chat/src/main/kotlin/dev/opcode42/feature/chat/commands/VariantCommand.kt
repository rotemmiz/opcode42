package dev.opcode42.feature.chat.commands

/** `/variant` — pick a variant for the current model (opens the variant picker). */
object VariantCommand : BuiltinCommand {
    override val name = "variant"
    override val description = "Pick a model variant"
    override val opensSubmenu = true
    override fun isAvailable(actions: ChatCommandActions): Boolean = actions.hasVariants
    override fun execute(actions: ChatCommandActions) = actions.openVariantPicker()
}
