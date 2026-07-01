package dev.opcode42.feature.chat.commands

import dev.opcode42.core.model.CommandInfo

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

    /** True when picking this entry opens a further picker — the palette shows a chevron. */
    val hasSubmenu: Boolean

    /**
     * Stable, collision-free list key. A built-in and a daemon command may share a [name]
     * (e.g. a user-defined `/diff`); namespacing the key keeps `LazyColumn` keys unique.
     */
    val key: String

    data class Builtin(
        val command: BuiltinCommand,
        override val enabled: Boolean,
    ) : PaletteEntry {
        override val name: String get() = command.name
        override val description: String? get() = command.description
        override val badge: String? get() = if (enabled) null else "soon"
        override val hasSubmenu: Boolean get() = command.opensSubmenu
        override val key: String get() = "builtin:$name"
    }

    data class Daemon(val info: CommandInfo) : PaletteEntry {
        override val name: String get() = info.name
        override val description: String? get() = info.description
        override val enabled: Boolean get() = true
        override val badge: String? get() = info.source?.takeIf { it == "mcp" || it == "skill" }
        override val hasSubmenu: Boolean get() = false
        override val key: String get() = "daemon:$name"
    }
}

/**
 * Merges built-in actions ahead of daemon commands (builtins-first ordering, like
 * the TUI's `filterSlash`). Unbuilt built-ins are kept but disabled;
 * implemented-but-currently-unavailable built-ins (e.g. `/terminal` without a
 * directory) are hidden. Daemon commands are de-duplicated by name so the merged
 * list always has collision-free [PaletteEntry.key]s.
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
        .distinctBy { it.name }
        .map { PaletteEntry.Daemon(it) }
    return builtinEntries + daemonEntries
}

/** Case-insensitive substring match on the command name (keeps the prior `contains` feel). */
fun List<PaletteEntry>.filterByQuery(query: String): List<PaletteEntry> =
    if (query.isEmpty()) this
    else filter { it.name.contains(query, ignoreCase = true) }
