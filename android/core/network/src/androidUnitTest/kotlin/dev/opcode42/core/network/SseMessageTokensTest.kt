package dev.opcode42.core.network

import dev.opcode42.core.model.AppEvent
import dev.opcode42.core.model.CacheUsage
import dev.opcode42.core.model.SseEvent
import dev.opcode42.core.model.TokenUsage
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
        // The wire's `total` is parsed and is what the gauge uses.
        assertEquals(54598.0, tokens.total)
        assertEquals(54598L, tokens.contextFootprint)
    }

    @Test
    fun contextFootprint_prefersTotal_elseSumsExcludingReasoning() {
        // No `total` → sum input+output+cache, reasoning excluded.
        val noTotal = TokenUsage(input = 1000.0, output = 50.0, reasoning = 9999.0, cache = CacheUsage(read = 200.0, write = 0.0))
        assertEquals(1250L, noTotal.contextFootprint)
        // `total` present and positive → preferred verbatim.
        assertEquals(7777L, noTotal.copy(total = 7777.0).contextFootprint)
        // `total` present but zero (provider hasn't reported it) → fall through to the
        // sum, matching the daemon's `*t.Total > 0` guard (overflow.go).
        assertEquals(1250L, noTotal.copy(total = 0.0).contextFootprint)
        // A turn that has only just started reports zeros → zero footprint, so the
        // gauge-selection rule skips it and keeps the previous turn's value.
        assertEquals(0L, TokenUsage().contextFootprint)
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
