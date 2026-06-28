package dev.forge.feature.chat.commands

import dev.forge.core.model.CommandInfo

/**
 * A single row in the `/` palette: either a client-side [BuiltinCommand] or a
 * daemon command from `GET /command`. This is the view-facing model the composer
 * renders and filters; routing on pick happens in `ChatScreen`.
 */
sealed interface PaletteEntry {
    /** Name without the leading slash. */
    val name: String
    val description: String?

    /** False → greyed and not selectable (a not-yet-implemented built-in). */
    val enabled: Boolean

    /** Optional trailing badge: "soon" for unbuilt built-ins, "mcp"/"skill" for daemon sources. */
    val badge: String?

    data class Builtin(
        val command: BuiltinCommand,
        override val enabled: Boolean,
    ) : PaletteEntry {
        override val name: String get() = command.name
        override val description: String? get() = command.description
        override val badge: String? get() = if (enabled) null else "soon"
    }

    data class Daemon(val info: CommandInfo) : PaletteEntry {
        override val name: String get() = info.name
        override val description: String? get() = info.description
        override val enabled: Boolean get() = true
        override val badge: String? get() = info.source?.takeIf { it == "mcp" || it == "skill" }
    }
}

/**
 * Merges built-in actions ahead of daemon commands (mirrors the TUI's
 * `filterSlash`, builtins first). Unbuilt built-ins are kept but disabled;
 * implemented-but-currently-unavailable built-ins (e.g. `/terminal` without a
 * directory) are hidden.
 */
fun buildPaletteEntries(
    builtins: List<BuiltinCommand>,
    daemon: List<CommandInfo>,
    actions: ChatCommandActions,
): List<PaletteEntry> {
    val builtinEntries = builtins.mapNotNull { cmd ->
        when {
            !cmd.implemented -> PaletteEntry.Builtin(cmd, enabled = false)
            cmd.isAvailable(actions) -> PaletteEntry.Builtin(cmd, enabled = true)
            else -> null
        }
    }
    val daemonEntries = daemon
        .filter { it.name.isNotEmpty() }
        .map { PaletteEntry.Daemon(it) }
    return builtinEntries + daemonEntries
}

/** Case-insensitive substring match on the command name (keeps the prior `contains` feel). */
fun List<PaletteEntry>.filterByQuery(query: String): List<PaletteEntry> =
    if (query.isEmpty()) this
    else filter { it.name.contains(query, ignoreCase = true) }
