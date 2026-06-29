package dev.opcode42.feature.chat.commands

/** `/agents` — switch the agent for upcoming prompts (opens the model + agent picker). */
object AgentsCommand : BuiltinCommand {
    override val name = "agents"
    override val description = "Switch agent"
    override fun execute(actions: ChatCommandActions) = actions.openModelPicker()
}
