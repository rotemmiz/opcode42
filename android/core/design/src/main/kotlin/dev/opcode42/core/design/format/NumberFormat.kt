package dev.opcode42.core.design.format

import java.util.Locale

/**
 * Compact human count: `1234 → "1.2K"`, `2_400_000 → "2.4M"`, `< 1000 → the plain integer`.
 *
 * The single source of truth for token/count formatting across screens (previously duplicated as
 * `formatTokens` in `:app` and `formatTokenCount` in `:feature:chat`). Always formats in
 * [Locale.US] so the decimal separator is a dot regardless of device locale.
 *
 * Note: this is deliberately distinct from `:feature:chat`'s `formatContextWindow`, which rounds
 * model context sizes to whole units ("200K", "2M") — a different presentation contract.
 */
fun formatCompactCount(n: Long): String = when {
    n >= 1_000_000 -> String.format(Locale.US, "%.1fM", n / 1_000_000.0)
    n >= 1_000 -> String.format(Locale.US, "%.1fK", n / 1_000.0)
    else -> n.toString()
}

/** [Double] overload for token totals carried as doubles; truncates to a whole count. */
fun formatCompactCount(n: Double): String = formatCompactCount(n.toLong())
