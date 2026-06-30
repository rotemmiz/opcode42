package dev.opcode42.core.model

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.descriptors.buildClassSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.*
import kotlinx.serialization.modules.SerializersModule

/** Deserializes a Part from JSON using the "type" discriminator field. */
object PartSerializer : KSerializer<Part> {
    override val descriptor: SerialDescriptor = buildClassSerialDescriptor("Part")

    override fun deserialize(decoder: Decoder): Part {
        val json = (decoder as JsonDecoder).decodeJsonElement().jsonObject
        val type = json["type"]?.jsonPrimitive?.content ?: "unknown"
        val id = json["id"]?.jsonPrimitive?.content ?: ""
        val sessionID = json["sessionID"]?.jsonPrimitive?.content ?: ""
        val messageID = json["messageID"]?.jsonPrimitive?.content ?: ""
        val format = decoder.json
        // A malformed typed part (e.g. a `file` part missing the required `url`) must
        // degrade to UnknownPart rather than throw — one bad part cannot be allowed to
        // abort the whole message/session decode. REST + SSE callers already wrap this,
        // but the model serializer should be the last line of defense on its own.
        val fallback = UnknownPart(id, sessionID, messageID, type)
        fun guard(decode: () -> Part): Part =
            try { decode() } catch (_: SerializationException) { fallback }
        return when (type) {
            "text" -> guard { format.decodeFromJsonElement(TextPart.serializer(), json) }
            "reasoning" -> guard { format.decodeFromJsonElement(ReasoningPart.serializer(), json) }
            "file" -> guard { format.decodeFromJsonElement(FilePart.serializer(), json) }
            "tool" -> guard { format.decodeFromJsonElement(ToolPartJson.serializer(), json).toPart() }
            "patch" -> guard { format.decodeFromJsonElement(PatchPart.serializer(), json) }
            "step-start" -> StepStartPart(id, sessionID, messageID)
            "step-finish" -> StepFinishPart(id, sessionID, messageID)
            else -> fallback
        }
    }

    override fun serialize(encoder: Encoder, value: Part) =
        throw SerializationException("Part serialization not supported")
}

/** Deserializes ToolState using the "status" discriminator. */
object ToolStateSerializer : KSerializer<ToolState> {
    override val descriptor: SerialDescriptor = buildClassSerialDescriptor("ToolState")

    override fun deserialize(decoder: Decoder): ToolState {
        val json = (decoder as JsonDecoder).decodeJsonElement().jsonObject
        val status = json["status"]?.jsonPrimitive?.content ?: "pending"
        val fmt = decoder.json
        return when (status) {
            "pending" -> fmt.decodeFromJsonElement(ToolStatePending.serializer(), json)
            "running" -> fmt.decodeFromJsonElement(ToolStateRunning.serializer(), json)
            "completed" -> fmt.decodeFromJsonElement(ToolStateCompleted.serializer(), json)
            "error" -> fmt.decodeFromJsonElement(ToolStateError.serializer(), json)
            else -> ToolStatePending(status)
        }
    }

    override fun serialize(encoder: Encoder, value: ToolState) =
        throw SerializationException("ToolState serialization not supported")
}

/** Deserializes Message (UserMessage | AssistantMessage) using the "role" field. */
object MessageSerializer : KSerializer<Message> {
    override val descriptor: SerialDescriptor = buildClassSerialDescriptor("Message")

    override fun deserialize(decoder: Decoder): Message {
        val json = (decoder as JsonDecoder).decodeJsonElement().jsonObject
        val role = json["role"]?.jsonPrimitive?.content ?: "assistant"
        val fmt = decoder.json
        return when (role) {
            "user" -> fmt.decodeFromJsonElement(UserMessage.serializer(), json).toMessage()
            else -> fmt.decodeFromJsonElement(AssistantMessage.serializer(), json).toMessage()
        }
    }

    override fun serialize(encoder: Encoder, value: Message) =
        throw SerializationException("Message serialization not supported")
}

// Intermediate Serializable for ToolPart (needs custom ToolState deserializer)
@kotlinx.serialization.Serializable
private data class ToolPartJson(
    val id: String,
    val sessionID: String,
    val messageID: String,
    val type: String = "tool",
    val callID: String,
    val tool: String,
    @kotlinx.serialization.Serializable(with = ToolStateSerializer::class)
    val state: ToolState,
) {
    fun toPart() = ToolPart(id, sessionID, messageID, type, callID, tool, state)
}

// Make UserMessage.parts and AssistantMessage.parts deserialize correctly via PartSerializer
@kotlinx.serialization.Serializable
private data class UserMessageJson(
    val id: String,
    val sessionID: String,
    val role: String = "user",
    val time: MessageTime = MessageTime(0),
    @kotlinx.serialization.Serializable(with = PartListSerializer::class)
    val parts: List<Part> = emptyList(),
    val format: String? = null,
    val agent: String? = null,
    val model: MessageModel? = null,
    val system: String? = null,
)

object PartListSerializer : KSerializer<List<Part>> {
    private val delegateSerializer = kotlinx.serialization.builtins.ListSerializer(PartSerializer)
    override val descriptor = delegateSerializer.descriptor
    override fun deserialize(decoder: Decoder) = delegateSerializer.deserialize(decoder)
    override fun serialize(encoder: Encoder, value: List<Part>) = delegateSerializer.serialize(encoder, value)
}

val Opcode42Json = Json {
    ignoreUnknownKeys = true
    isLenient = true
    coerceInputValues = true
    serializersModule = SerializersModule {
        contextual(Part::class, PartSerializer)
        contextual(ToolState::class, ToolStateSerializer)
    }
}
