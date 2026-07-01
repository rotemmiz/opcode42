package dev.opcode42.feature.chat.commands

/** `/agents` — switch the agent mode for upcoming prompts (opens the agent picker). */
object AgentsCommand : BuiltinCommand {
    override val name = "agents"
    override val description = "Change agent mode"
    override val opensSubmenu = true
    override fun execute(actions: ChatCommandActions) = actions.openAgentPicker()
}
