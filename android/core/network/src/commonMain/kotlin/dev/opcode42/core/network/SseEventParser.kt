package dev.opcode42.core.network

import dev.opcode42.core.model.*
import kotlinx.serialization.json.*

/**
 * Converts raw SseEvent envelopes into typed AppEvent instances.
 * Uses the event's "type" field as a discriminator.
 *
 * Pure multiplatform logic (commonMain) — no Android or DI dependencies. On
 * Android it is provided as a Hilt `@Singleton` via NetworkModule.
 */
class SseEventParser {
    fun parse(raw: SseEvent): AppEvent = try {
        val p = raw.properties
        when (raw.type) {
            "server.connected" -> AppEvent.ServerConnected
            "server.heartbeat" -> AppEvent.ServerHeartbeat
            "global.disposed" -> AppEvent.GlobalDisposed

            // session.created / session.updated wrap the session under `info`
            // (opencode openapi EventSessionUpdated.properties = {sessionID, info}).
            "session.updated", "session.created" ->
                AppEvent.SessionUpdated(
                    Opcode42Json.decodeFromJsonElement(Session.serializer(), p["info"] ?: p))
            // opencode/Opcode42 emit `session.deleted`; keep `session.removed` as an alias.
            "session.deleted", "session.removed" ->
                AppEvent.SessionRemoved(
                    p["sessionID"]?.jsonPrimitive?.content
                        ?: (p["info"] as? JsonObject)?.get("id")?.jsonPrimitive?.content ?: "")
            "session.status" ->
                AppEvent.SessionStatus(
                    sessionId = p["sessionID"]?.jsonPrimitive?.content ?: "",
                    status = p["status"]?.jsonObject?.get("type")?.jsonPrimitive?.content ?: "idle",
                )

            // message.updated wraps the message info under `info`
            // (EventMessageUpdated.properties = {sessionID, info}).
            "message.updated" ->
                AppEvent.MessageUpdated(parseMessage((p["info"] as? JsonObject) ?: p))
            // EventMessageRemoved.properties = {sessionID, messageID}.
            "message.removed" ->
                AppEvent.MessageRemoved(
                    sessionId = p["sessionID"]?.jsonPrimitive?.content ?: "",
                    messageId = p["messageID"]?.jsonPrimitive?.content
                        ?: p["id"]?.jsonPrimitive?.content ?: "",
                )

            // EventMessagePartUpdated.properties = {sessionID, part, time} —
            // the part object is nested under `part`.
            "message.part.updated" ->
                AppEvent.PartUpdated(parsePart((p["part"] as? JsonObject) ?: p))
            // EventMessagePartRemoved.properties = {sessionID, messageID, partID}.
            "message.part.removed" ->
                AppEvent.PartRemoved(
                    partId = p["partID"]?.jsonPrimitive?.content
                        ?: p["id"]?.jsonPrimitive?.content ?: "",
                    messageId = p["messageID"]?.jsonPrimitive?.content ?: "",
                )
            // EventMessagePartDelta.properties = {sessionID, messageID, partID, field, delta}.
            "message.part.delta" ->
                AppEvent.PartDelta(
                    partId = p["partID"]?.jsonPrimitive?.content
                        ?: p["id"]?.jsonPrimitive?.content ?: "",
                    messageId = p["messageID"]?.jsonPrimitive?.content ?: "",
                    delta = p["delta"]?.jsonPrimitive?.content ?: "",
                )

            "permission.asked" ->
                AppEvent.PermissionAsked(Opcode42Json.decodeFromJsonElement(PermissionRequest.serializer(), p))
            "permission.replied" ->
                AppEvent.PermissionReplied(p["requestID"]?.jsonPrimitive?.content
                    ?: p["id"]?.jsonPrimitive?.content ?: "")

            "question.asked" ->
                AppEvent.QuestionAsked(Opcode42Json.decodeFromJsonElement(QuestionRequest.serializer(), p))
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

    // The role/type discriminator dispatch lives once, in the model's MessageSerializer /
    // PartSerializer (registered on Opcode42Json). Both the REST mapper and this SSE parser
    // decode through them so the two wire paths can never drift — and both inherit
    // PartSerializer's "malformed part degrades to UnknownPart" guard.
    private fun parseMessage(json: JsonObject): Message =
        Opcode42Json.decodeFromJsonElement(MessageSerializer, json)

    private fun parsePart(json: JsonObject): Part =
        Opcode42Json.decodeFromJsonElement(PartSerializer, json)
}
