package dev.opcode42.feature.sessions

import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

private val DATE_HEADER_FORMAT: DateTimeFormatter =
    DateTimeFormatter.ofPattern("EEE MMM d yyyy", Locale.US)
private val SHORT_DATE_FORMAT: DateTimeFormatter =
    DateTimeFormatter.ofPattern("MMM d", Locale.US)

/**
 * Date-group header for a session's last-active time: `"Today"` / `"Yesterday"` (compared in
 * the local zone), otherwise an absolute `"Sat Jun 27 2026"` — matching opencode's Sessions
 * view. `now` and `zone` are injectable so the grouping is deterministically unit-testable.
 */
fun dateBucket(
    epochMs: Long,
    now: Long = System.currentTimeMillis(),
    zone: ZoneId = ZoneId.systemDefault(),
): String {
    if (epochMs <= 0L) return "Earlier"
    val date = Instant.ofEpochMilli(epochMs).atZone(zone).toLocalDate()
    val today = Instant.ofEpochMilli(now).atZone(zone).toLocalDate()
    return when (date) {
        today -> "Today"
        today.minusDays(1) -> "Yesterday"
        else -> DATE_HEADER_FORMAT.format(date)
    }
}

/**
 * Compact "last active" label (Claude Code style): `"now"`, `"3m"`, `"2h"`, `"4d"`, then a
 * short `"Jun 27"` for anything a week or older. Empty for a missing/zero timestamp.
 */
fun relativeTime(
    epochMs: Long,
    now: Long = System.currentTimeMillis(),
    zone: ZoneId = ZoneId.systemDefault(),
): String {
    if (epochMs <= 0L) return ""
    val diff = now - epochMs
    if (diff < 60_000) return "now"
    val mins = diff / 60_000
    val hours = diff / 3_600_000
    val days = diff / 86_400_000
    return when {
        mins < 60 -> "${mins}m"
        hours < 24 -> "${hours}h"
        days < 7 -> "${days}d"
        else -> SHORT_DATE_FORMAT.format(Instant.ofEpochMilli(epochMs).atZone(zone).toLocalDate())
    }
}
