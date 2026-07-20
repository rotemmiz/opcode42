package dev.opcode42.feature.chat.commands

/**
 * The capability surface a built-in slash command may invoke. It is the boundary
 * between a [BuiltinCommand] (pure logic, one per file) and `ChatScreen`, which
 * owns the sheet/dialog state and navigation callbacks and implements this.
 *
 * Commands never touch Compose state or the ViewModel directly — they call one of
 * these methods, keeping the `commands` package free of Android/Compose types and
 * unit-testable with a fake.
 */
interface ChatCommandActions {
    /** Whether the session has a working directory (runtime gate for `/terminal`). */
    val hasDirectory: Boolean

    /** Whether the current model exposes selectable variants (runtime gate for `/variant`). */
    val hasVariants: Boolean

    fun newSession()
    /** Multi-pane: open the nav rail; compact: navigate back to the session list. */
    fun openSessions()
    /** `/models` opens the model picker (provider-grouped). */
    fun openModelPicker()
    /** `/variant` opens the variant picker for the current model. */
    fun openVariantPicker()
    /** `/agents` opens the agent-mode picker. */
    fun openAgentPicker()
    /** `/stash` opens the stashed-drafts sheet (local-only persistence). */
    fun openStash()
    fun openTerminal()
    fun openInfo()
    /** `/diff` opens the session diff viewer (working-tree changes or session patches). */
    fun openDiffViewer()
    fun renameSession()
    fun forkSession()
    fun summarize()
    fun shareSession()
    /** Confirmed via a dialog before the session is archived. */
    fun archiveSession()
    /** Confirmed via a dialog before the session is deleted. */
    fun deleteSession()
}
