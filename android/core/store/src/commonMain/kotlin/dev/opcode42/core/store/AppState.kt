package dev.opcode42.core.store

import dev.opcode42.core.model.Message
import dev.opcode42.core.model.Part
import dev.opcode42.core.model.PermissionRequest
import dev.opcode42.core.model.QuestionRequest
import dev.opcode42.core.model.Session
import dev.opcode42.core.model.SnapshotFileDiff

/** Top-level immutable application state — mirrors GlobalStore + TUI sync store. */
data class AppState(
    /** All known sessions, sorted by ID (lexicographically monotonic). */
    val sessions: List<Session> = emptyList(),

    /** sessionID → sorted message list */
    val messages: Map<String, List<Message>> = emptyMap(),

    /** messageID → sorted part list */
    val parts: Map<String, List<Part>> = emptyMap(),

    /** sessionID → pending permission requests */
    val permissions: Map<String, List<PermissionRequest>> = emptyMap(),

    /** sessionID → pending question requests */
    val questions: Map<String, List<QuestionRequest>> = emptyMap(),

    /** sessionID → status string ("running" | "idle" | etc.) */
    val sessionStatus: Map<String, String> = emptyMap(),

    /** sessionID → list of optimistic (un-confirmed) messages awaiting server echo */
    val optimisticMessages: Map<String, List<OptimisticMessage>> = emptyMap(),

    val connectionState: ConnectionState = ConnectionState.Disconnected,

    /** messageID → diff file list loaded from /session/{id}/diff */
    val diffs: Map<String, List<SnapshotFileDiff>> = emptyMap(),
)

data class OptimisticMessage(
    val id: String,
    val sessionID: String,
    val text: String,
    val confirmed: Boolean = false,
)

sealed class ConnectionState {
    data object Disconnected : ConnectionState()
    data object Connecting : ConnectionState()
    data object Connected : ConnectionState()
    data class Failed(val cause: Throwable? = null) : ConnectionState()
}
