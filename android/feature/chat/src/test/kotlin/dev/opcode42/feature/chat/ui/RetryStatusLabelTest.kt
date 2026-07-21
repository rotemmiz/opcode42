package dev.opcode42.feature.chat.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Guards PR 3.3 — `session.status` with `type: "retry"` must surface "Retrying" (and the
 * attempt count when the wire carries it) instead of the old generic "busy" busy state.
 *
 * Wire shape: the opencode `SessionStatus` retry variant (openapi.json:17278-17324) carries
 * a required integer `attempt` but no max. The TUI renders "Retrying (attempt N)"
 * (tui/src/routes/session/index.tsx:2316); we match that wording, and never fabricate a
 * count when the event omits it.
 */
class RetryStatusLabelTest {

    @Test
    fun nonRetryStatus_returnsNull() {
        assertNull(retryStatusLabel("busy", null))
        assertNull(retryStatusLabel("idle", null))
        assertNull(retryStatusLabel("running", null))
    }

    @Test
    fun nullStatus_returnsNull() {
        assertNull(retryStatusLabel(null, null))
    }

    @Test
    fun retryWithoutAttempt_returnsBareRetrying() {
        assertEquals("Retrying", retryStatusLabel("retry", null))
    }

    @Test
    fun retryWithAttempt_returnsRetryingWithAttempt() {
        assertEquals("Retrying (attempt 2)", retryStatusLabel("retry", 2))
    }

    @Test
    fun retryWithAttemptOne_showsAttemptOne() {
        assertEquals("Retrying (attempt 1)", retryStatusLabel("retry", 1))
    }

    @Test
    fun retryWithZeroAttempt_fallsBackToBareRetrying() {
        // attempt is `minimum: 0` on the wire; a 0 is not a meaningful "attempt N", so fall
        // back to the bare label rather than render "Retrying (attempt 0)".
        assertEquals("Retrying", retryStatusLabel("retry", 0))
    }

    @Test
    fun retryWithNegativeAttempt_fallsBackToBareRetrying() {
        assertEquals("Retrying", retryStatusLabel("retry", -1))
    }
}