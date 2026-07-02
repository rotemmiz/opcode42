package dev.opcode42.core.design.theme

import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

/**
 * Shared typography tokens — one consistent line-height ratio across the app instead of
 * per-call-site literals. Body text uses 1.4× (M3 default); code/terminal keep a tighter 1.5×
 * ratio intentionally. Use these instead of raw `lineHeight =` so the ratio is centralized.
 *
 * Font sizes mirror the values previously hardcoded at each call site, now with a sane
 * lineHeight derived from the ratio rather than an ad-hoc number.
 */
object Opcode42Typography {
    // Body — 1.4× ratio (M3 default)
    val bodyLarge = TextStyle(fontSize = 16.sp, lineHeight = 22.4.sp)
    val bodyMedium = TextStyle(fontSize = 14.5.sp, lineHeight = 20.3.sp)
    val bodySmall = TextStyle(fontSize = 13.5.sp, lineHeight = 18.9.sp)

    // Labels — 1.45× ratio (slightly tighter for caps/labels)
    val labelLarge = TextStyle(fontSize = 14.sp, fontWeight = FontWeight.Medium, lineHeight = 20.3.sp)
    val labelMedium = TextStyle(fontSize = 12.sp, fontWeight = FontWeight.Medium, lineHeight = 17.4.sp)
    val labelSmall = TextStyle(fontSize = 11.sp, fontWeight = FontWeight.Bold, lineHeight = 16.sp)

    // Code / mono — 1.5× ratio (mono fonts read better with more breathing room)
    val code = TextStyle(fontFamily = Opcode42Mono, fontSize = 12.sp, lineHeight = 18.sp)
    val codeSmall = TextStyle(fontFamily = Opcode42Mono, fontSize = 11.sp, lineHeight = 16.5.sp)

    // Terminal — 1.33× (dense, like a real terminal)
    val terminal = TextStyle(fontFamily = Opcode42Mono, fontSize = 12.sp, lineHeight = 16.sp)
}

/**
 * Spacing scale — replaces ad-hoc dp literals with named tokens so paddings are consistent
 * and Material-aligned. `sm`/`md`/`lg`/`xl` cover the common cases; use raw dp only for
 * one-off layout math.
 */
object Opcode42Spacing {
    val xs = androidx.compose.ui.unit.Dp(4f)
    val sm = androidx.compose.ui.unit.Dp(8f)
    val md = androidx.compose.ui.unit.Dp(16f)
    val lg = androidx.compose.ui.unit.Dp(24f)
    val xl = androidx.compose.ui.unit.Dp(32f)
}