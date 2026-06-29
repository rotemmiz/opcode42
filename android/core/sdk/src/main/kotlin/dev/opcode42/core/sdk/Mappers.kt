package dev.opcode42.core.sdk

import dev.opcode42.core.model.AssistantError
import dev.opcode42.core.model.FilePart
import dev.opcode42.core.model.Message
import dev.opcode42.core.model.MessageTime
import dev.opcode42.core.model.Opcode42Json
import dev.opcode42.core.model.Part
import dev.opcode42.core.model.PatchPart
import dev.opcode42.core.model.ReasoningPart
import dev.opcode42.core.model.StepFinishPart
import dev.opcode42.core.model.StepStartPart
import dev.opcode42.core.model.TextPart
import dev.opcode42.core.model.TokenUsage
import dev.opcode42.core.model.ToolPart
import dev.opcode42.core.model.UnknownPart
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
import kotlinx.serialization.json.longOrNull

/**
 * Pure JSON→domain mapping for the REST client. No I/O — unit-testable in isolation
 * (see `MappersTest`). Tolerant by design: a malformed part/message is skipped, not fatal,
 * mirroring how the daemon's stream can race ahead of a client's model.
 */

/** Maps a single `part` object to its [Part] subtype; unknown `type`s become [UnknownPart]. */
internal fun parsePart(obj: JsonObject): Part {
    val type = obj["type"]?.jsonPrimitive?.content ?: "unknown"
    val id = obj["id"]?.jsonPrimitive?.content ?: ""
    val sessionID = obj["sessionID"]?.jsonPrimitive?.content ?: ""
    val messageID = obj["messageID"]?.jsonPrimitive?.content ?: ""
    return when (type) {
        "text" -> Opcode42Json.decodeFromJsonElement(TextPart.serializer(), obj)
        "reasoning" -> Opcode42Json.decodeFromJsonElement(ReasoningPart.serializer(), obj)
        "file" -> Opcode42Json.decodeFromJsonElement(FilePart.serializer(), obj)
        "tool" -> Opcode42Json.decodeFromJsonElement(ToolPart.serializer(), obj)
        "patch" -> Opcode42Json.decodeFromJsonElement(PatchPart.serializer(), obj)
        "step-start" -> StepStartPart(id, sessionID, messageID)
        "step-finish" -> StepFinishPart(id, sessionID, messageID)
        else -> UnknownPart(id, sessionID, messageID, type)
    }
}

/**
 * Maps one element of `GET /session/{id}/message` to a [Message]. opencode wraps each message as
 * `{ info: {...}, parts: [...] }` (falls back to a flat object). Returns null when the element is
 * unusable (no id, or a structural error), so callers can `mapNotNull`.
 */
internal fun parseMessage(elem: kotlinx.serialization.json.JsonElement, fallbackSessionId: String): Message? =
    try {
        val obj = elem.jsonObject
        val info = obj["info"]?.jsonObject ?: obj // fall back to flat if unwrapped
        val partsArr = obj["parts"]?.jsonArray ?: JsonArray(emptyList())
        val role = info["role"]?.jsonPrimitive?.content ?: "assistant"
        val id = info["id"]?.jsonPrimitive?.content ?: return null
        val sessionID = info["sessionID"]?.jsonPrimitive?.content ?: fallbackSessionId
        val timeObj = info["time"]?.jsonObject
        val time = MessageTime(
            created = timeObj?.get("created")?.jsonPrimitive?.long ?: 0L,
            completed = timeObj?.get("completed")?.jsonPrimitive?.longOrNull,
        )
        val parts = partsArr.mapNotNull { p ->
            try { parsePart(p.jsonObject) } catch (_: Exception) { null }
        }
        // opencode sends these flat on the message info, not nested.
        val modelID = info["modelID"]?.jsonPrimitive?.contentOrNull
        val providerID = info["providerID"]?.jsonPrimitive?.contentOrNull
        val mode = info["mode"]?.jsonPrimitive?.contentOrNull
        val agent = info["agent"]?.jsonPrimitive?.contentOrNull
        val error = if (role == "assistant") {
            info["error"]?.let { Opcode42Json.decodeFromJsonElement(AssistantError.serializer(), it) }
        } else null
        val isSummary = role == "assistant" &&
            info["summary"]?.jsonPrimitive?.booleanOrNull == true
        // Per-turn token usage (assistant turns) — drives the live context gauge.
        val tokens = if (role == "assistant") {
            info["tokens"]?.let {
                runCatching { Opcode42Json.decodeFromJsonElement(TokenUsage.serializer(), it) }.getOrNull()
            }
        } else null
        Message(
            id = id, sessionID = sessionID, role = role, time = time,
            parts = parts, error = error, modelID = modelID,
            providerID = providerID, mode = mode, agent = agent,
            tokens = tokens, isSummary = isSummary,
        )
    } catch (_: Exception) {
        null
    }
