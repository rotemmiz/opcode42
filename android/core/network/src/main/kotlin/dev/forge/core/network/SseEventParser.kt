package dev.forge.core.network

import dev.forge.core.model.*
import kotlinx.serialization.json.*
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Converts raw SseEvent envelopes into typed AppEvent instances.
 * Uses the event's "type" field as a discriminator.
 */
@Singleton
class SseEventParser @Inject constructor() {
    fun parse(raw: SseEvent): AppEvent = try {
        val p = raw.properties
        when (raw.type) {
            "server.connected" -> AppEvent.ServerConnected
            "server.heartbeat" -> AppEvent.ServerHeartbeat
            "global.disposed" -> AppEvent.GlobalDisposed

            "session.updated" ->
                AppEvent.SessionUpdated(ForgeJson.decodeFromJsonElement(Session.serializer(), p))
            "session.removed" ->
                AppEvent.SessionRemoved(p["sessionID"]?.jsonPrimitive?.content ?: "")

            "message.updated" ->
                AppEvent.MessageUpdated(parseMessage(p))
            "message.removed" ->
                AppEvent.MessageRemoved(
                    sessionId = p["sessionID"]?.jsonPrimitive?.content ?: "",
                    messageId = p["id"]?.jsonPrimitive?.content ?: "",
                )

            "message.part.updated" ->
                AppEvent.PartUpdated(parsePart(p))
            "message.part.removed" ->
                AppEvent.PartRemoved(p["id"]?.jsonPrimitive?.content ?: "")
            "message.part.delta" ->
                AppEvent.PartDelta(
                    partId = p["id"]?.jsonPrimitive?.content ?: "",
                    messageId = p["messageID"]?.jsonPrimitive?.content ?: "",
                    delta = p["delta"]?.jsonPrimitive?.content ?: "",
                )

            "permission.asked" ->
                AppEvent.PermissionAsked(ForgeJson.decodeFromJsonElement(PermissionRequest.serializer(), p))
            "permission.replied" ->
                AppEvent.PermissionReplied(p["requestID"]?.jsonPrimitive?.content
                    ?: p["id"]?.jsonPrimitive?.content ?: "")

            "question.asked" ->
                AppEvent.QuestionAsked(ForgeJson.decodeFromJsonElement(QuestionRequest.serializer(), p))
            "question.replied" ->
                AppEvent.QuestionReplied(p["requestID"]?.jsonPrimitive?.content
                    ?: p["id"]?.jsonPrimitive?.content ?: "")
            "question.rejected" ->
                AppEvent.QuestionRejected(p["requestID"]?.jsonPrimitive?.content
                    ?: p["id"]?.jsonPrimitive?.content ?: "")

            else -> AppEvent.Unknown(raw)
        }
    } catch (e: Exception) {
        AppEvent.Unknown(raw)
    }

    private fun parseMessage(json: JsonObject): Message {
        val role = json["role"]?.jsonPrimitive?.content ?: "assistant"
        return when (role) {
            "user" -> ForgeJson.decodeFromJsonElement(UserMessage.serializer(), json).toMessage()
            else -> ForgeJson.decodeFromJsonElement(AssistantMessage.serializer(), json).toMessage()
        }
    }

    private fun parsePart(json: JsonObject): Part {
        val type = json["type"]?.jsonPrimitive?.content ?: "unknown"
        val id = json["id"]?.jsonPrimitive?.content ?: ""
        val sessionID = json["sessionID"]?.jsonPrimitive?.content ?: ""
        val messageID = json["messageID"]?.jsonPrimitive?.content ?: ""
        return when (type) {
            "text" -> ForgeJson.decodeFromJsonElement(TextPart.serializer(), json)
            "reasoning" -> ForgeJson.decodeFromJsonElement(ReasoningPart.serializer(), json)
            "file" -> ForgeJson.decodeFromJsonElement(FilePart.serializer(), json)
            "step-start" -> StepStartPart(id, sessionID, messageID)
            "step-finish" -> StepFinishPart(id, sessionID, messageID)
            else -> UnknownPart(id, sessionID, messageID, type)
        }
    }
}
