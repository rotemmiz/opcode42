package dev.forge.feature.chat.commands

/** `/variant` — pick a model variant. Not yet implemented on Android (shown disabled). */
object VariantCommand : BuiltinCommand {
    override val name = "variant"
    override val description = "Pick a model variant"
    override val implemented = false
    override fun execute(actions: ChatCommandActions) = Unit
}
