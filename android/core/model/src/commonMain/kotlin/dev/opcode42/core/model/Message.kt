package dev.opcode42.core.model

import kotlinx.serialization.Contextual
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

@Serializable
data class UserMessage(
    val id: String,
    val sessionID: String,
    val role: String = "user",
    val time: MessageTime = MessageTime(0),
    val parts: List<@Contextual Part> = emptyList(),
    val format: String? = null,
    val agent: String? = null,
    val model: MessageModel? = null,
    val system: String? = null,
)

@Serializable
data class AssistantMessage(
    val id: String,
    val sessionID: String,
    val role: String = "assistant",
    val time: MessageTime = MessageTime(0),
    val parts: List<@Contextual Part> = emptyList(),
    val error: AssistantError? = null,
    // opencode sends these flat on the message (not a nested `model` object).
    val modelID: String? = null,
    val providerID: String? = null,
    val mode: String? = null,
    val agent: String? = null,
    /** Per-turn token usage for THIS assistant message (opencode sends it on the
     *  message). Used to report live context-window occupancy — distinct from the
     *  session's cumulative `tokens`. */
    val tokens: TokenUsage? = null,
    /** True when this assistant message is a context-compaction summary. */
    val summary: Boolean? = null,
)

@Serializable
data class MessageTime(
    val created: Long,
    val completed: Long? = null,
)

@Serializable
data class MessageModel(
    val providerID: String,
    val modelID: String,
    val variant: String? = null,
)

@Serializable
data class AssistantError(
    val name: String? = null,
    val message: String? = null,
)

/** Unified message envelope — carries whichever role variant was returned. */
data class Message(
    val id: String,
    val sessionID: String,
    val role: String,
    val time: MessageTime,
    val parts: List<Part>,
    val error: AssistantError? = null,
    val modelID: String? = null,
    val providerID: String? = null,
    val mode: String? = null,
    val agent: String? = null,
    /** Per-turn token usage (assistant turns only); see [AssistantMessage.tokens]. */
    val tokens: TokenUsage? = null,
    /** True when this assistant message is a context-compaction summary. */
    val isSummary: Boolean = false,
)

fun UserMessage.toMessage() = Message(
    id = id,
    sessionID = sessionID,
    role = role,
    time = time,
    parts = parts,
    agent = agent,
)

fun AssistantMessage.toMessage() = Message(
    id = id,
    sessionID = sessionID,
    role = role,
    time = time,
    parts = parts,
    error = error,
    modelID = modelID,
    providerID = providerID,
    mode = mode,
    agent = agent,
    tokens = tokens,
    isSummary = summary == true,
)
