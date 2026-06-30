package dev.opcode42.feature.chat.commands

/**
 * The Android client's built-in slash actions, in palette order: navigation/mode
 * actions, then session-management actions, then the not-yet-implemented set
 * (rendered disabled). Mirrors `builtinCommands` in `internal/tui/slash.go`; the
 * subset reflects what the Android client can actually do today.
 */
val builtinCommands: List<BuiltinCommand> = listOf(
    NewSessionCommand,
    SessionsCommand,
    ModelsCommand,
    AgentsCommand,
    TerminalCommand,
    InfoCommand,
    RenameCommand,
    ForkCommand,
    SummarizeCommand,
    ShareCommand,
    ArchiveCommand,
    DeleteCommand,
    // Coming soon — shown disabled until the supporting screens land.
    DiffCommand,
    TimelineCommand,
    VariantCommand,
    StashCommand,
)
