package dev.opcode42.feature.chat.commands

/** `/timeline` — revert to a turn. Not yet implemented on Android (shown disabled). */
object TimelineCommand : BuiltinCommand {
    override val name = "timeline"
    override val description = "Revert to a turn"
    override val implemented = false
    override fun execute(actions: ChatCommandActions) = Unit
}
