package dev.opcode42.core.sdk

import dev.opcode42.core.model.Opcode42Json
import dev.opcode42.core.model.StepStartPart
import dev.opcode42.core.model.TextPart
import dev.opcode42.core.model.ToolPart
import dev.opcode42.core.model.UnknownPart
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pins the JSON→domain mapping that `Opcode42Client.getMessages` relies on (extracted from the
 * client into `Mappers.kt`). Feeds real-shaped `GET /session/{id}/message` elements and asserts
 * the [dev.opcode42.core.model.Message]/[dev.opcode42.core.model.Part] tree.
 */
class MappersTest {

    private fun element(json: String) = Opcode42Json.parseToJsonElement(json.trimIndent())

    @Test fun parsesWrappedAssistantMessage_withFlatModelFieldsTokensAndParts() {
        val msg = parseMessage(
            element(
                """
                {
                  "info": {
                    "id": "msg_2", "sessionID": "ses_1", "role": "assistant",
                    "time": {"created": 100, "completed": 200},
                    "modelID": "claude-x", "providerID": "anthropic", "mode": "build",
                    "tokens": {"input": 10, "output": 5},
                    "summary": false
                  },
                  "parts": [
                    {"type": "step-start", "id": "p0", "sessionID": "ses_1", "messageID": "msg_2"},
                    {"type": "text", "id": "p1", "sessionID": "ses_1", "messageID": "msg_2", "text": "hi"},
                    {"type": "tool", "id": "p2", "sessionID": "ses_1", "messageID": "msg_2",
                     "tool": "bash", "callID": "c1", "state": {"status": "completed", "input": {}, "output": "ok"}},
                    {"type": "wat", "id": "p3", "sessionID": "ses_1", "messageID": "msg_2"}
                  ]
                }
                """,
            ),
            fallbackSessionId = "ignored",
        )!!

        assertEquals("msg_2", msg.id)
        assertEquals("ses_1", msg.sessionID)
        assertEquals("assistant", msg.role)
        assertEquals(100L, msg.time.created)
        assertEquals(200L, msg.time.completed)
        assertEquals("claude-x", msg.modelID)
        assertEquals("anthropic", msg.providerID)
        assertEquals("build", msg.mode)
        assertEquals(10.0, msg.tokens!!.input, 0.0)
        assertEquals(5.0, msg.tokens!!.output, 0.0)
        assertEquals(false, msg.isSummary)

        assertEquals(4, msg.parts.size)
        assertTrue(msg.parts[0] is StepStartPart)
        assertEquals("hi", (msg.parts[1] as TextPart).text)
        assertEquals("bash", (msg.parts[2] as ToolPart).tool)
        // Unknown part types degrade gracefully, preserving the raw `type`.
        assertEquals("wat", (msg.parts[3] as UnknownPart).type)
    }

    @Test fun fallsBackToFlatObject_whenNoInfoWrapper() {
        val msg = parseMessage(
            element(
                """
                {"id": "msg_1", "sessionID": "ses_9", "role": "user", "time": {"created": 1}}
                """,
            ),
            fallbackSessionId = "ignored",
        )!!

        assertEquals("msg_1", msg.id)
        assertEquals("ses_9", msg.sessionID)
        assertEquals("user", msg.role)
        // Non-assistant messages never carry tokens/error.
        assertNull(msg.tokens)
    }

    @Test fun usesFallbackSessionId_whenAbsent() {
        val msg = parseMessage(
            element("""{"info": {"id": "msg_3", "role": "assistant"}}"""),
            fallbackSessionId = "ses_fallback",
        )!!
        assertEquals("ses_fallback", msg.sessionID)
    }

    @Test fun assistantSummaryFlagIsRead() {
        val msg = parseMessage(
            element("""{"info": {"id": "msg_4", "role": "assistant", "summary": true}}"""),
            fallbackSessionId = "ses_1",
        )!!
        assertTrue(msg.isSummary)
    }

    @Test fun returnsNull_whenIdMissing() {
        assertNull(parseMessage(element("""{"info": {"role": "assistant"}}"""), "ses_1"))
    }
}
