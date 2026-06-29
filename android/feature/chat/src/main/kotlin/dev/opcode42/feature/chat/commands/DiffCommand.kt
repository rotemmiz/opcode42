package dev.opcode42.feature.chat.commands

/** `/diff` — review session changes. Not yet implemented on Android (shown disabled). */
object DiffCommand : BuiltinCommand {
    override val name = "diff"
    override val description = "Review session changes"
    override val implemented = false
    override fun execute(actions: ChatCommandActions) = Unit
}
