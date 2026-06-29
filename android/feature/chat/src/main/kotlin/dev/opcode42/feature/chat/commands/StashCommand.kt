package dev.opcode42.feature.chat.commands

/** `/stash` — stashed prompt drafts. Not yet implemented on Android (shown disabled). */
object StashCommand : BuiltinCommand {
    override val name = "stash"
    override val description = "Stashed prompt drafts"
    override val implemented = false
    override fun execute(actions: ChatCommandActions) = Unit
}
