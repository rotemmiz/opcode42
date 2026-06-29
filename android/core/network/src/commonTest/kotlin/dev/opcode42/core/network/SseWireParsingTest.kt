package dev.opcode42.core.network

import dev.opcode42.core.model.AppEvent
import dev.opcode42.core.model.SseEvent
import dev.opcode42.core.model.TextPart
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Wire-compat tests for the Android SSE consumption path, pinned to the exact
 * Opcode42/opencode payload shapes:
 *
 *  - Every SSE frame uses the `event: message` name; the typed event lives in
 *    the JSON `data` body (Opcode42 internal/server/sse.go writeSSE).
 *  - `/event` (instance) sends the bare `{id,type,properties}`.
 *  - `/global/event` wraps it as `{payload:{...}, directory, project, workspace}`.
 *  - Property field names match openapi.json (EventMessagePartUpdated.part,
 *    EventMessagePartDelta.partID, EventMessageUpdated.info, …).
 *
 * These guard the bugs fixed in plan-07 Phase B: previously the code used the
 * SSE event-name ("message") as the type and read top-level part/partID fields.
 */
class SseWireParsingTest {

    private val parser = SseEventParser()

    // ─── parseSseData: envelope unwrapping ──────────────────────────────────

    @Test
    fun `instance stream parses bare envelope`() {
        val data = """{"id":"evt_1","type":"server.connected","properties":{}}"""
        val ev = parseSseData(data)!!
        assertEquals("server.connected", ev.type)
        assertEquals("evt_1", ev.id)
        assertNull(ev.directory)
    }

    @Test
    fun `global stream unwraps payload and surfaces directory`() {
        val data = """
            {"payload":{"id":"evt_2","type":"session.status",
              "properties":{"sessionID":"ses_1","status":{"type":"running"}}},
             "directory":"/work/proj"}
        """.trimIndent()
        val ev = parseSseData(data)!!
        assertEquals("session.status", ev.type)
        assertEquals("evt_2", ev.id)
        assertEquals("/work/proj", ev.directory)
        assertEquals("ses_1", ev.properties["sessionID"]?.jsonPrimitive?.content)
    }

    @Test
    fun `data with no type returns null`() {
        assertNull(parseSseData("""{"properties":{}}"""))
    }

    @Test
    fun `non-object data returns null`() {
        assertNull(parseSseData("""[1,2,3]"""))
        assertNull(parseSseData("""not json"""))
    }

    @Test
    fun `does not use the SSE event-name as the type`() {
        // The old bug: type was taken from OkHttp's SSE `event:` field ("message").
        val ev = parseSseData("""{"type":"message.removed","properties":{}}""")!!
        assertEquals("message.removed", ev.type)
    }

    // ─── SseEventParser: field-name fixes ───────────────────────────────────

    @Test
    fun `message_part_updated reads nested part object`() {
        // Opcode42: properties = {sessionID, part:{...}, time}
        val raw = sse(
            "message.part.updated",
            """{"sessionID":"ses_1",
               "part":{"id":"prt_1","sessionID":"ses_1","messageID":"msg_1",
                       "type":"text","text":"hello"},
               "time":123}""",
        )
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.PartUpdated)
        val part = (ev as AppEvent.PartUpdated).part
        assertEquals("prt_1", part.id)
        assertEquals("msg_1", part.messageID)
        assertTrue(part is TextPart)
        assertEquals("hello", (part as TextPart).text)
    }

    @Test
    fun `message_part_delta reads partID not id`() {
        // Opcode42: properties = {sessionID, messageID, partID, field, delta}
        val raw = sse(
            "message.part.delta",
            """{"sessionID":"ses_1","messageID":"msg_1","partID":"prt_9",
               "field":"text","delta":"chunk"}""",
        )
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.PartDelta)
        ev as AppEvent.PartDelta
        assertEquals("prt_9", ev.partId)
        assertEquals("msg_1", ev.messageId)
        assertEquals("chunk", ev.delta)
    }

    @Test
    fun `message_part_removed reads partID`() {
        val raw = sse(
            "message.part.removed",
            """{"sessionID":"ses_1","messageID":"msg_1","partID":"prt_7"}""",
        )
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.PartRemoved)
        assertEquals("prt_7", (ev as AppEvent.PartRemoved).partId)
    }

    @Test
    fun `message_updated reads message under info`() {
        // EventMessageUpdated.properties = {sessionID, info:{<message>}}
        val raw = sse(
            "message.updated",
            """{"sessionID":"ses_1",
               "info":{"id":"msg_5","sessionID":"ses_1","role":"assistant",
                       "time":{"created":1},"modelID":"m","providerID":"p"}}""",
        )
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.MessageUpdated)
        val msg = (ev as AppEvent.MessageUpdated).message
        assertEquals("msg_5", msg.id)
        assertEquals("assistant", msg.role)
    }

    @Test
    fun `message_removed reads messageID`() {
        val raw = sse("message.removed", """{"sessionID":"ses_1","messageID":"msg_3"}""")
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.MessageRemoved)
        assertEquals("msg_3", (ev as AppEvent.MessageRemoved).messageId)
    }

    @Test
    fun `session_updated reads session under info`() {
        // EventSessionUpdated.properties = {sessionID, info:{<session>}}
        val raw = sse(
            "session.updated",
            """{"sessionID":"ses_8","info":{"id":"ses_8","title":"My session"}}""",
        )
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.SessionUpdated)
        val s = (ev as AppEvent.SessionUpdated).session
        assertEquals("ses_8", s.id)
        assertEquals("My session", s.title)
    }

    @Test
    fun `session_updated surfaces time_archived as a number`() {
        // opencode types time.archived as a finite number; the session list filters on it.
        // A live session.updated after PATCH {time:{archived}} carries it under info.time.
        val raw = sse(
            "session.updated",
            """{"sessionID":"ses_a",
               "info":{"id":"ses_a","title":"Old","time":{"created":1,"archived":1717000000000}}}""",
        )
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.SessionUpdated)
        val s = (ev as AppEvent.SessionUpdated).session
        assertEquals("ses_a", s.id)
        assertEquals(1717000000000L, s.time?.archived)
    }

    @Test
    fun `session_updated without archived leaves it null`() {
        val raw = sse(
            "session.updated",
            """{"sessionID":"ses_b","info":{"id":"ses_b","time":{"created":1}}}""",
        )
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.SessionUpdated)
        assertNull((ev as AppEvent.SessionUpdated).session.time?.archived)
    }

    @Test
    fun `session_created is handled like session_updated`() {
        val raw = sse("session.created", """{"sessionID":"ses_9","info":{"id":"ses_9"}}""")
        assertTrue(parser.parse(raw) is AppEvent.SessionUpdated)
    }

    @Test
    fun `session_deleted maps to SessionRemoved`() {
        val raw = sse("session.deleted", """{"sessionID":"ses_2"}""")
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.SessionRemoved)
        assertEquals("ses_2", (ev as AppEvent.SessionRemoved).sessionId)
    }

    @Test
    fun `session_status reads nested status type`() {
        val raw = sse(
            "session.status",
            """{"sessionID":"ses_1","status":{"type":"running"}}""",
        )
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.SessionStatus)
        ev as AppEvent.SessionStatus
        assertEquals("ses_1", ev.sessionId)
        assertEquals("running", ev.status)
    }

    @Test
    fun `permission_asked decodes request directly from properties`() {
        // EventPermissionAsked.properties IS the PermissionRequest.
        val raw = sse(
            "permission.asked",
            """{"id":"per_1","sessionID":"ses_1","title":"Run rm"}""",
        )
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.PermissionAsked)
        val p = (ev as AppEvent.PermissionAsked).permission
        assertEquals("per_1", p.id)
        assertEquals("ses_1", p.sessionID)
    }

    @Test
    fun `permission_replied reads requestID`() {
        val raw = sse(
            "permission.replied",
            """{"sessionID":"ses_1","requestID":"per_1","reply":"once"}""",
        )
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.PermissionReplied)
        assertEquals("per_1", (ev as AppEvent.PermissionReplied).requestId)
    }

    @Test
    fun `question_replied reads requestID`() {
        val raw = sse(
            "question.replied",
            """{"sessionID":"ses_1","requestID":"qst_1","answers":[]}""",
        )
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.QuestionReplied)
        assertEquals("qst_1", (ev as AppEvent.QuestionReplied).requestId)
    }

    @Test
    fun `unknown event type falls through to Unknown`() {
        assertTrue(parser.parse(sse("some.future.event", "{}")) is AppEvent.Unknown)
    }

    // ─── end-to-end: data string → typed AppEvent ───────────────────────────

    @Test
    fun `global frame end to end yields typed part update`() {
        val data = """
            {"payload":{"id":"evt_3","type":"message.part.updated",
              "properties":{"sessionID":"ses_1",
                "part":{"id":"prt_1","sessionID":"ses_1","messageID":"msg_1",
                        "type":"text","text":"hi"},"time":1}},
             "directory":"/w"}
        """.trimIndent()
        val raw = parseSseData(data)!!
        val ev = parser.parse(raw)
        assertTrue(ev is AppEvent.PartUpdated)
        assertEquals("prt_1", (ev as AppEvent.PartUpdated).part.id)
    }

    private fun sse(type: String, propsJson: String): SseEvent =
        SseEvent(
            id = "evt",
            type = type,
            properties = Opcode42JsonParse(propsJson),
        )
}

/** Minimal helper to build a JsonObject from a raw JSON string in tests. */
private fun Opcode42JsonParse(s: String) =
    kotlinx.serialization.json.Json.parseToJsonElement(s) as kotlinx.serialization.json.JsonObject
