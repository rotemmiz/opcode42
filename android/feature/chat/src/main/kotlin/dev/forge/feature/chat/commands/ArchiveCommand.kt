package dev.forge.feature.chat.commands

/** `/archive` — archive the current session (asks for confirmation first). */
object ArchiveCommand : BuiltinCommand {
    override val name = "archive"
    override val description = "Archive session"
    override fun execute(actions: ChatCommandActions) = actions.archiveSession()
}
