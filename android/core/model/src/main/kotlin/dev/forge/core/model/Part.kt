package dev.forge.core.model

import kotlinx.serialization.Contextual
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

/** Discriminated union over all part types. Deserialized via PartDeserializer. */
sealed class Part {
    abstract val id: String
    abstract val sessionID: String
    abstract val messageID: String
    abstract val type: String
}

@Serializable
data class TextPart(
    override val id: String,
    override val sessionID: String,
    override val messageID: String,
    override val type: String = "text",
    val text: String,
    val synthetic: Boolean? = null,
    val ignored: Boolean? = null,
    val time: PartTime? = null,
) : Part()

@Serializable
data class ReasoningPart(
    override val id: String,
    override val sessionID: String,
    override val messageID: String,
    override val type: String = "reasoning",
    val text: String,
    val time: PartTime? = null,
) : Part()

@Serializable
data class FilePart(
    override val id: String,
    override val sessionID: String,
    override val messageID: String,
    override val type: String = "file",
    val mime: String,
    val filename: String? = null,
    val url: String,
) : Part()

@Serializable
data class ToolPart(
    override val id: String,
    override val sessionID: String,
    override val messageID: String,
    override val type: String = "tool",
    val callID: String,
    val tool: String,
    @Contextual val state: ToolState,
) : Part()

@Serializable
data class StepStartPart(
    override val id: String,
    override val sessionID: String,
    override val messageID: String,
    override val type: String = "step-start",
) : Part()

@Serializable
data class StepFinishPart(
    override val id: String,
    override val sessionID: String,
    override val messageID: String,
    override val type: String = "step-finish",
) : Part()

/** Catch-all for part types we don't explicitly model yet. */
data class UnknownPart(
    override val id: String,
    override val sessionID: String,
    override val messageID: String,
    override val type: String,
) : Part()

@Serializable
data class PartTime(
    val start: Long,
    val end: Long? = null,
)

// ─── Tool state discriminated union ───────────────────────────────────────────

sealed class ToolState {
    abstract val status: String
}

@Serializable
data class ToolStatePending(
    override val status: String = "pending",
    val input: JsonObject? = null,
) : ToolState()

@Serializable
data class ToolStateRunning(
    override val status: String = "running",
    val input: JsonObject? = null,
    val title: String? = null,
    val time: PartTime? = null,
) : ToolState()

@Serializable
data class ToolStateCompleted(
    override val status: String = "completed",
    val input: JsonObject? = null,
    val output: String? = null,
    val title: String? = null,
    val time: PartTime? = null,
) : ToolState()

@Serializable
data class ToolStateError(
    override val status: String = "error",
    val input: JsonObject? = null,
    val error: String? = null,
    val time: PartTime? = null,
) : ToolState()
