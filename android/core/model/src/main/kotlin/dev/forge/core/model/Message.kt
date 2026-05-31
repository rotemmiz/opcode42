package dev.forge.core.model

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
    val model: MessageModel? = null,
    val agent: String? = null,
    val summary: MessageSummary? = null,
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

@Serializable
data class MessageSummary(
    val title: String? = null,
    val body: String? = null,
)

/** Unified message envelope — carries whichever role variant was returned. */
data class Message(
    val id: String,
    val sessionID: String,
    val role: String,
    val time: MessageTime,
    val parts: List<Part>,
    val error: AssistantError? = null,
    val model: MessageModel? = null,
    val agent: String? = null,
)

fun UserMessage.toMessage() = Message(
    id = id,
    sessionID = sessionID,
    role = role,
    time = time,
    parts = parts,
    model = model,
    agent = agent,
)

fun AssistantMessage.toMessage() = Message(
    id = id,
    sessionID = sessionID,
    role = role,
    time = time,
    parts = parts,
    error = error,
    model = model,
    agent = agent,
)
