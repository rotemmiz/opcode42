package dev.forge.core.network

import dev.forge.core.model.AppEvent
import dev.forge.core.model.SseEvent
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

/**
 * Regression for the context-window gauge: a `message.updated` for an assistant turn
 * must surface its per-turn `tokens` so the panel can show live context occupancy
 * (NOT the session's cumulative lifetime total). The wire carries an extra `total`
 * field inside `tokens` that must be ignored, not rejected.
 */
class SseMessageTokensTest {
    @Test
    fun assistantMessageTokens_areParsedForContextGauge() {
        val info = """
            {"id":"msg_1","sessionID":"ses_1","role":"assistant","time":{"created":1},
             "modelID":"glm-5.2","providerID":"ollama-cloud","mode":"build",
             "tokens":{"total":54598,"input":54351,"output":247,"reasoning":0,"cache":{"read":0,"write":0}}}
        """.trimIndent()
        val props = Json.parseToJsonElement("""{"sessionID":"ses_1","info":$info}""").jsonObject
        val event = SseEvent(type = "message.updated", properties = props)

        val parsed = SseEventParser().parse(event)
        assertTrue(parsed is AppEvent.MessageUpdated)
        val tokens = (parsed as AppEvent.MessageUpdated).message.tokens
        assertNotNull(tokens)
        assertEquals(54351.0, tokens.input, 0.0)
        assertEquals(247.0, tokens.output, 0.0)
        // The unknown `total` field is dropped, and the summed footprint equals it.
        val footprint = tokens.input + tokens.output + tokens.reasoning + tokens.cache.read + tokens.cache.write
        assertEquals(54598.0, footprint, 0.0)
    }

    @Test
    fun userMessage_hasNoTokens() {
        val info = """{"id":"msg_u","sessionID":"ses_1","role":"user","time":{"created":1}}"""
        val props = Json.parseToJsonElement("""{"sessionID":"ses_1","info":$info}""").jsonObject
        val parsed = SseEventParser().parse(SseEvent(type = "message.updated", properties = props))
        assertTrue(parsed is AppEvent.MessageUpdated)
        assertEquals(null, (parsed as AppEvent.MessageUpdated).message.tokens)
    }
}
