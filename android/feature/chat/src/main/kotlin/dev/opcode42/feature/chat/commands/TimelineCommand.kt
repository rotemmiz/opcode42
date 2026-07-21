package dev.opcode42.feature.chat.commands

/** `/timeline` — revert to a turn (opens the session timeline sheet). */
object TimelineCommand : BuiltinCommand {
    override val name = "timeline"
    override val description = "Revert to a turn"
    override val opensSubmenu = true
    override fun execute(actions: ChatCommandActions) = actions.openTimeline()
}