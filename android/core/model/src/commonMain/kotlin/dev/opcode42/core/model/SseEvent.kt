package dev.opcode42.core.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

/** Raw SSE event envelope — shape: { id, type, properties, directory? } */
@Serializable
data class SseEvent(
    val id: String? = null,
    val type: String,
    val properties: JsonObject = JsonObject(emptyMap()),
    val directory: String? = null,
)

/** Typed wrappers for events the app handles. */
sealed class AppEvent {
    /** Server lifecycle */
    data object ServerConnected : AppEvent()
    data object ServerHeartbeat : AppEvent()
    data object GlobalDisposed : AppEvent()

    /** Session */
    data class SessionUpdated(val session: Session) : AppEvent()
    data class SessionRemoved(val sessionId: String) : AppEvent()
    data class SessionStatus(val sessionId: String, val status: String) : AppEvent()

    /** Message */
    data class MessageUpdated(val message: Message) : AppEvent()
    data class MessageRemoved(val sessionId: String, val messageId: String) : AppEvent()

    /** Part */
    data class PartUpdated(val part: Part) : AppEvent()
    data class PartRemoved(val partId: String) : AppEvent()
    data class PartDelta(val partId: String, val messageId: String, val delta: String) : AppEvent()

    /** Permission / question */
    data class PermissionAsked(val permission: PermissionRequest) : AppEvent()
    data class PermissionReplied(val requestId: String) : AppEvent()
    data class QuestionAsked(val question: QuestionRequest) : AppEvent()
    data class QuestionReplied(val requestId: String) : AppEvent()
    data class QuestionRejected(val requestId: String) : AppEvent()

    /** Diff data loaded from /session/{id}/diff */
    data class SessionDiffLoaded(val messageId: String, val diffs: List<SnapshotFileDiff>) : AppEvent()

    /** Unrecognized event — stored for debug */
    data class Unknown(val raw: SseEvent) : AppEvent()
}

@Serializable
data class PermissionRequest(
    val id: String,
    val sessionID: String,
    val title: String? = null,
    val description: String? = null,
    val metadata: JsonObject? = null,
)

@Serializable
data class QuestionRequest(
    val id: String,
    val sessionID: String,
    val message: String? = null,
)
