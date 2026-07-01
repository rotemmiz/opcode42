package dev.opcode42.core.store

import dev.opcode42.core.model.*
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertSame

private fun msg(id: String, sessionID: String = "s1", parts: List<Part> = emptyList()) =
    Message(id = id, sessionID = sessionID, role = "assistant", time = MessageTime(0), parts = parts)

private fun textPart(id: String, msgID: String = "m1") =
    TextPart(id = id, sessionID = "s1", messageID = msgID, text = "hello")

class StoreReducerTest {

    // ─── MessageUpdated ────────────────────────────────────────────────────────

    @Test
    fun messageUpdated_insertsNewMessage() {
        val state = AppState()
        val m = msg("m1")
        val next = reduce(state, AppEvent.MessageUpdated(m))
        assertEquals(listOf(m), next.messages["s1"])
    }

    @Test
    fun messageUpdated_withParts_replacesExistingParts() {
        val old = msg("m1", parts = listOf(textPart("p1")))
        val state = AppState(messages = mapOf("s1" to listOf(old)))
        val fresh = msg("m1", parts = listOf(textPart("p2")))
        val next = reduce(state, AppEvent.MessageUpdated(fresh))
        assertEquals(listOf(textPart("p2")), next.messages["s1"]?.first()?.parts)
    }

    @Test
    fun messageUpdated_emptyParts_preservesExistingParts() {
        val part = textPart("p1")
        val old = msg("m1", parts = listOf(part))
        val state = AppState(messages = mapOf("s1" to listOf(old)))
        // SSE metadata-only event has no parts
        val metadataOnly = msg("m1", parts = emptyList())
        val next = reduce(state, AppEvent.MessageUpdated(metadataOnly))
        assertEquals(listOf(part), next.messages["s1"]?.first()?.parts)
    }

    @Test
    fun messageUpdated_emptyParts_noExisting_insertsAsIs() {
        val state = AppState()
        val m = msg("m1", parts = emptyList())
        val next = reduce(state, AppEvent.MessageUpdated(m))
        assertEquals(emptyList<Part>(), next.messages["s1"]?.first()?.parts)
    }

    @Test
    fun messageUpdated_emptyParts_existingHasNoParts_insertsAsIs() {
        val old = msg("m1", parts = emptyList())
        val state = AppState(messages = mapOf("s1" to listOf(old)))
        val metadataOnly = msg("m1", parts = emptyList())
        val next = reduce(state, AppEvent.MessageUpdated(metadataOnly))
        assertEquals(emptyList<Part>(), next.messages["s1"]?.first()?.parts)
    }

    @Test
    fun messageUpdated_maintainsSortOrder() {
        val m2 = msg("m2")
        val state = AppState(messages = mapOf("s1" to listOf(m2)))
        val m1 = msg("m1")
        val next = reduce(state, AppEvent.MessageUpdated(m1))
        assertEquals(listOf("m1", "m2"), next.messages["s1"]?.map { it.id })
    }

    // ─── PartUpdated ───────────────────────────────────────────────────────────

    @Test
    fun partUpdated_insertsNewPart() {
        val state = AppState()
        val part = textPart("p1")
        val next = reduce(state, AppEvent.PartUpdated(part))
        assertEquals(listOf(part), next.parts["m1"])
    }

    @Test
    fun partUpdated_replacesExistingPart() {
        val old = textPart("p1")
        val state = AppState(parts = mapOf("m1" to listOf(old)))
        val updated = old.copy(text = "updated")
        val next = reduce(state, AppEvent.PartUpdated(updated))
        assertEquals("updated", (next.parts["m1"]?.first() as? TextPart)?.text)
    }

    // ─── PartDelta / PartRemoved — indexed by carried messageId ─────────────────

    @Test
    fun partDelta_appendsToTargetPart_viaCarriedMessageId() {
        val state = AppState(parts = mapOf("m1" to listOf(textPart("p1"))))
        val next = reduce(state, AppEvent.PartDelta(partId = "p1", messageId = "m1", delta = " world"))
        assertEquals("hello world", (next.parts["m1"]?.first() as? TextPart)?.text)
    }

    @Test
    fun partDelta_onlyMutatesTargetMessageBucket() {
        // Two message buckets; a delta for m2/p2 must leave m1 untouched (by identity).
        val m1Parts = listOf(textPart("p1", msgID = "m1"))
        val state = AppState(parts = mapOf("m1" to m1Parts, "m2" to listOf(textPart("p2", msgID = "m2"))))
        val next = reduce(state, AppEvent.PartDelta(partId = "p2", messageId = "m2", delta = "!"))
        assertSame(m1Parts, next.parts["m1"])
        assertEquals("hello!", (next.parts["m2"]?.first() as? TextPart)?.text)
    }

    @Test
    fun partDelta_fallsBackToScan_whenMessageIdBlank() {
        val state = AppState(parts = mapOf("m1" to listOf(textPart("p1"))))
        val next = reduce(state, AppEvent.PartDelta(partId = "p1", messageId = "", delta = "!"))
        assertEquals("hello!", (next.parts["m1"]?.first() as? TextPart)?.text)
    }

    @Test
    fun partDelta_unknownPart_isNoOp() {
        val state = AppState(parts = mapOf("m1" to listOf(textPart("p1"))))
        val next = reduce(state, AppEvent.PartDelta(partId = "ghost", messageId = "mX", delta = "!"))
        assertSame(state, next)
    }

    @Test
    fun partRemoved_dropsPart_viaCarriedMessageId() {
        val state = AppState(parts = mapOf("m1" to listOf(textPart("p1"), textPart("p2"))))
        val next = reduce(state, AppEvent.PartRemoved(partId = "p1", messageId = "m1"))
        assertEquals(listOf(textPart("p2")), next.parts["m1"])
    }

    @Test
    fun partRemoved_fallsBackToScan_whenMessageIdBlank() {
        val state = AppState(parts = mapOf("m1" to listOf(textPart("p1"), textPart("p2"))))
        val next = reduce(state, AppEvent.PartRemoved(partId = "p1", messageId = ""))
        assertEquals(listOf(textPart("p2")), next.parts["m1"])
    }

    @Test
    fun messageRemoved_dropsMessage_viaCarriedSessionId() {
        val state = AppState(messages = mapOf("s1" to listOf(msg("m1"), msg("m2"))))
        val next = reduce(state, AppEvent.MessageRemoved(sessionId = "s1", messageId = "m1"))
        assertEquals(listOf(msg("m2")), next.messages["s1"])
    }
}
