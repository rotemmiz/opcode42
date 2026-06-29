package dev.opcode42.core.network

import dev.opcode42.core.model.Opcode42Json
import dev.opcode42.core.model.SseEvent
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive

/**
 * Parses one SSE `data:` JSON string into a typed [SseEvent].
 *
 * Both Opcode42 and opencode emit every frame with the SSE `event:` name "message"
 * and carry the actual event in the JSON body:
 *
 *  - Instance stream (`/event`):   `{ "id", "type", "properties" }` (bare).
 *  - Global stream (`/global/event`): `{ "payload": { "id","type","properties" },
 *    "directory", "project", "workspace" }`.
 *
 * This unwraps the `payload` envelope when present and surfaces the routing
 * `directory` so downstream consumers see a uniform [SseEvent]. Returns null if
 * the body is not a JSON object or carries no `type`.
 *
 * Refs: Opcode42 internal/server/sse.go (writeSSE → `event: message`; global wraps
 * in {payload,directory}); opencode bus/global.ts:5-8, bus/index.ts:103.
 *
 * Pure multiplatform logic (commonMain): no Android, OkHttp or DI dependencies.
 */
internal fun parseSseData(data: String, fallbackId: String? = null): SseEvent? {
    val root = try {
        Opcode42Json.parseToJsonElement(data) as? JsonObject ?: return null
    } catch (_: Exception) {
        return null
    }
    // Unwrap the global envelope: {payload:{...}, directory, ...}
    val payload = (root["payload"] as? JsonObject)
    val body = payload ?: root
    val directory = root["directory"]?.jsonPrimitive?.contentOrNull
    val type = body["type"]?.jsonPrimitive?.contentOrNull ?: return null
    val id = body["id"]?.jsonPrimitive?.contentOrNull ?: fallbackId
    val properties = (body["properties"] as? JsonObject) ?: JsonObject(emptyMap())
    return SseEvent(id = id, type = type, properties = properties, directory = directory)
}
