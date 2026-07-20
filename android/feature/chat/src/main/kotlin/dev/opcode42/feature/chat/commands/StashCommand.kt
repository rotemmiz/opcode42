package dev.opcode42.feature.chat.commands

/** `/stash` — stashed prompt drafts (local-only DataStore). Opens the stash sheet. */
object StashCommand : BuiltinCommand {
    override val name = "stash"
    override val description = "Stashed prompt drafts"
    override val opensSubmenu = true
    override fun execute(actions: ChatCommandActions) = actions.openStash()
}
