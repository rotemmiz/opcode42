package dev.opcode42.feature.chat.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Guards PR 3.3 — `session.status` with `type: "retry"` must surface "Retrying" (and the
 * attempt count when the wire carries it) instead of the old generic "busy" busy state.
 *
 * Wire shape: the opencode `SessionStatus` retry variant (openapi.json:17278-17324) carries
 * a required integer `attempt` (minimum 0) but no max. The TUI renders "Retrying (attempt N)"
 * (tui/src/routes/session/index.tsx:2316) with no lower-bound guard; we match that wording
 * exactly (including attempt 0), and never fabricate a count when the event omits it.
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
    fun retryWithZeroAttempt_showsAttemptZero_matchingTui() {
        // attempt is `minimum: 0` on the wire and the TUI renders it without a lower-bound
        // guard (formatSubagentRetry just interpolates), so we match that exactly.
        assertEquals("Retrying (attempt 0)", retryStatusLabel("retry", 0))
    }
}
