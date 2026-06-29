package dev.opcode42.feature.sessions

import java.time.LocalDate
import java.time.ZoneId
import org.junit.Assert.assertEquals
import org.junit.Test

/** Deterministic coverage for the date-bucket + relative-time helpers (injected zone + now). */
class SessionTimeTest {

    private val zone = ZoneId.of("UTC")
    private val today = LocalDate.of(2026, 6, 27) // a Saturday (per opencode's Sessions view)
    private fun ms(date: LocalDate, hour: Int = 12) =
        date.atTime(hour, 0).atZone(zone).toInstant().toEpochMilli()
    private val now = ms(today)

    @Test fun dateBucket_todayYesterdayAbsolute() {
        assertEquals("Today", dateBucket(ms(today), now, zone))
        assertEquals("Yesterday", dateBucket(ms(today.minusDays(1)), now, zone))
        assertEquals("Mon Jun 22 2026", dateBucket(ms(today.minusDays(5)), now, zone))
        assertEquals("Earlier", dateBucket(0, now, zone))
    }

    @Test fun relativeTime_buckets() {
        assertEquals("now", relativeTime(now - 30_000, now, zone))
        assertEquals("3m", relativeTime(now - 3 * 60_000, now, zone))
        assertEquals("2h", relativeTime(now - 2 * 3_600_000, now, zone))
        assertEquals("4d", relativeTime(now - 4 * 86_400_000L, now, zone))
        assertEquals("", relativeTime(0, now, zone))
        // A week or older falls through to a short absolute date.
        assertEquals("Jun 7", relativeTime(ms(today.minusDays(20)), now, zone))
    }
}
