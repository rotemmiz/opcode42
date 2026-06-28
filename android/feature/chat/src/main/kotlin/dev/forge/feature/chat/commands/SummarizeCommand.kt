package dev.forge.feature.chat.commands

/** `/summarize` — compact the session context. */
object SummarizeCommand : BuiltinCommand {
    override val name = "summarize"
    override val description = "Summarize context"
    override fun execute(actions: ChatCommandActions) = actions.summarize()
}
