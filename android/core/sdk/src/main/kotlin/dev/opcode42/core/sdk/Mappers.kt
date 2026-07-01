package dev.opcode42.core.sdk

import dev.opcode42.core.model.Message
import dev.opcode42.core.model.MessageSerializer
import dev.opcode42.core.model.Opcode42Json
import dev.opcode42.core.model.PartSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject

/**
 * Pure JSON→domain mapping for the REST client. No I/O — unit-testable in isolation
 * (see `MappersTest`). Tolerant by design: a malformed part/message is skipped, not fatal,
 * mirroring how the daemon's stream can race ahead of a client's model.
 *
 * The role/type discriminator dispatch is NOT re-implemented here — it lives once in the model's
 * [MessageSerializer] / `PartSerializer` (registered on [Opcode42Json]), shared with the SSE
 * parser so the REST and stream paths can never drift.
 */

/**
 * Maps one element of `GET /session/{id}/message` to a [Message]. opencode wraps each message as
 * `{ info: {...}, parts: [...] }` (falls back to a flat object). Returns null when the element is
 * unusable (no id, or a structural error), so callers can `mapNotNull`.
 */
internal fun parseMessage(elem: JsonElement, fallbackSessionId: String): Message? =
    try {
        val obj = elem.jsonObject
        val info = obj["info"]?.jsonObject ?: obj // fall back to flat if unwrapped
        // `parts` is a sibling of `info` on the wire, so decode the metadata via MessageSerializer
        // and attach the parts (each via the contextual PartSerializer) afterwards.
        val partsArr = obj["parts"]?.jsonArray ?: JsonArray(emptyList())
        val base = Opcode42Json.decodeFromJsonElement(MessageSerializer, info)
        val parts = partsArr.mapNotNull { p ->
            runCatching { Opcode42Json.decodeFromJsonElement(PartSerializer, p) }.getOrNull()
        }
        base.copy(
            sessionID = base.sessionID.ifEmpty { fallbackSessionId },
            parts = parts,
        )
    } catch (_: Exception) {
        null
    }
