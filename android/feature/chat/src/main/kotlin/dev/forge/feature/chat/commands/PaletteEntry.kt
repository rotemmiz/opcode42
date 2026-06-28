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
        override val key: String get() = "builtin:$name"
    }

    data class Daemon(val info: CommandInfo) : PaletteEntry {
        override val name: String get() = info.name
        override val description: String? get() = info.description
        override val enabled: Boolean get() = true
        override val badge: String? get() = info.source?.takeIf { it == "mcp" || it == "skill" }
        override val key: String get() = "daemon:$name"
    }
}

/**
 * Builds the palette in three bands: available built-in actions first (in registry
 * order, like the TUI's builtins-first `filterSlash`), then daemon commands, then
 * the disabled "coming soon" built-ins last so they never bury real commands.
 * Implemented-but-currently-unavailable built-ins (e.g. `/terminal` without a
 * directory, `/info` when the panel is already shown) are hidden. Daemon commands
 * are de-duplicated by name so the merged list always has collision-free
 * [PaletteEntry.key]s.
 */
fun buildPaletteEntries(
    builtins: List<BuiltinCommand>,
    daemon: List<CommandInfo>,
    actions: ChatCommandActions,
): List<PaletteEntry> {
    val available = builtins
        .filter { it.implemented && it.isAvailable(actions) }
        .map { PaletteEntry.Builtin(it, enabled = true) }
    val daemonEntries = daemon
        .filter { it.name.isNotEmpty() }
        .distinctBy { it.name }
        .map { PaletteEntry.Daemon(it) }
    val comingSoon = builtins
        .filter { !it.implemented }
        .map { PaletteEntry.Builtin(it, enabled = false) }
    return available + daemonEntries + comingSoon
}

/** Case-insensitive substring match on the command name (keeps the prior `contains` feel). */
fun List<PaletteEntry>.filterByQuery(query: String): List<PaletteEntry> =
    if (query.isEmpty()) this
    else filter { it.name.contains(query, ignoreCase = true) }
