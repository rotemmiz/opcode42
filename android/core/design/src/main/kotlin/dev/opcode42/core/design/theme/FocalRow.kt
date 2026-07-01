package dev.opcode42.core.design.theme

import androidx.compose.foundation.background
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * The "amber focal row" — the app's canonical selected/active-row treatment
 * (design bundle: `secondaryContainer` amber tint + a start-inset amber accent
 * bar, `box-shadow: inset 2px 0 0 amber`). Used by the active diff-card header,
 * session rows, and the composer sheets (command palette, @-mention, model and
 * agent pickers) so selection reads identically everywhere.
 *
 * When [active] is false this is a no-op, so call sites can apply it
 * unconditionally: `Modifier.focalRow(active = selected)`.
 */
@Composable
fun Modifier.focalRow(active: Boolean, accentWidth: Dp = 2.5.dp): Modifier {
    if (!active) return this
    // drawBehind runs in the draw phase where the @Composable color getters aren't
    // callable, so read the accent here in composition and close over it.
    val accent = Secondary
    return this
        .background(SecondaryContainer)
        .drawBehind { drawRect(accent, size = Size(accentWidth.toPx(), size.height)) }
}
