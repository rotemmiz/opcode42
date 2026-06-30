package dev.opcode42.core.design.text

import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.text.BasicText
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.TextMeasurer
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.rememberTextMeasurer

/**
 * Single-line text truncated at the START with a leading ellipsis — e.g. `…/git/opcode42` — so the
 * most-specific tail (leaf dir, branch) stays visible. A manual stand-in for
 * `TextOverflow.StartEllipsis` (Compose 1.8+; this project pins 1.7.8): the slot width comes from
 * [BoxWithConstraints] and [fitStart] finds the longest fitting suffix, cached per (text, width, style).
 */
@Composable
fun StartEllipsisText(
    text: String,
    style: TextStyle,
    modifier: Modifier = Modifier,
) {
    val measurer = rememberTextMeasurer()
    BoxWithConstraints(modifier) {
        val maxW = constraints.maxWidth
        val shown = remember(text, maxW, style) { measurer.fitStart(text, style, maxW) }
        BasicText(text = shown, style = style, maxLines = 1, softWrap = false)
    }
}

/** Widest leading-ellipsis (`…tail`) form of [text] that fits [maxWidthPx]; full text if it all fits. */
fun TextMeasurer.fitStart(text: String, style: TextStyle, maxWidthPx: Int): String {
    if (maxWidthPx <= 0) return ""
    if (widthOf(text, style) <= maxWidthPx) return text
    var lo = 0
    var hi = text.length
    while (lo < hi) {
        val mid = (lo + hi + 1) / 2
        if (widthOf("…" + text.takeLast(mid), style) <= maxWidthPx) lo = mid else hi = mid - 1
    }
    return "…" + text.takeLast(lo)
}

private fun TextMeasurer.widthOf(s: String, style: TextStyle): Int =
    measure(s, style, softWrap = false, maxLines = 1).size.width
