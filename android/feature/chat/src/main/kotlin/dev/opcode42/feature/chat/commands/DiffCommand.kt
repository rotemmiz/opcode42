package dev.opcode42.feature.chat.commands

/** `/diff` — review session changes in a bottom sheet listing the changed files. */
object DiffCommand : BuiltinCommand {
    override val name = "diff"
    override val description = "Review session changes"
    override val implemented = true
    override val opensSubmenu = true
    override fun execute(actions: ChatCommandActions) = actions.openDiffViewer()
}
