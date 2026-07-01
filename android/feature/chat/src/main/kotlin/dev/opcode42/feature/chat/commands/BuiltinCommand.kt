package dev.opcode42.feature.chat.commands

/**
 * A built-in client action surfaced as a slash command (the Android counterpart of
 * the TUI's `slashBuiltin` items in `internal/tui/slash.go`). Built-ins are *not*
 * advertised by the daemon — each client owns its own list because they are local
 * UI operations the server cannot represent.
 *
 * Implementations are stateless [kotlin.objects][Any] (one per file) registered in
 * [builtinCommands].
 */
interface BuiltinCommand {
    /** Command name without the leading slash, e.g. "models". */
    val name: String

    /** One-line description shown beside the name in the palette. */
    val description: String

    /** When false the row renders greyed with a "soon" badge and is not selectable. */
    val implemented: Boolean get() = true

    /**
     * True when picking this command opens a further picker (a sub-sheet) rather
     * than acting immediately — the palette renders a trailing chevron for these.
     */
    val opensSubmenu: Boolean get() = false

    /**
     * Runtime gate for implemented commands (e.g. `/terminal` needs a directory). An
     * implemented command that is not currently available is hidden from the palette.
     */
    fun isAvailable(actions: ChatCommandActions): Boolean = true

    /** Performs the action against the screen's capability surface. */
    fun execute(actions: ChatCommandActions)
}
