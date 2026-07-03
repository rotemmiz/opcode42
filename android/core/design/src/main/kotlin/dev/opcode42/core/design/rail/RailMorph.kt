package dev.opcode42.core.design.rail

import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.RoundRect
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.clipPath
import androidx.compose.ui.unit.dp
import androidx.compose.ui.util.lerp

// ─── Shared collapsible-rail geometry ──────────────────────────────────────────
// The left nav rail morphs between an open [RailOpenWidth] and a collapsed [RailCollapsedWidth]
// icon band, driven by a `progress: () -> Float` provider (1f = open … 0f = collapsed) read ONLY
// inside draw/layout lambdas so the per-frame float never recomposes. These dimensions are the
// single source of truth shared by `Modifier.railWidth` (host), `Modifier.railContentWidth`
// (session rows), and [railActiveHighlight] below — they must agree for the clip-off-the-edge
// collapse to line up.

/** Open (expanded) rail width. M3 navigation-drawer spec: 220dp for inline-push is fine. */
val RailOpenWidth = 220.dp

/** Collapsed icon-band width. M3 navigation-rail default is 80dp (not the former 60). */
val RailCollapsedWidth = 80.dp

/** Edge of the collapsed glyph (the single-letter avatar / active square). */
val RailAvatarSize = 38.dp

/** Left inset that centers a [RailAvatarSize] glyph in the [RailCollapsedWidth] band: (60−38)/2. */
val RailLeftInset = (RailCollapsedWidth - RailAvatarSize) / 2

// Active-highlight tuning, lerped open ⇄ collapsed:
private val OpenInsetX = 8.dp // pill horizontal inset when open
private val OpenInsetY = 3.dp // pill vertical inset when open (floating capsule style)
private val OpenCorner = 16.dp
private val CollapsedCorner = 19.dp // perfect circle when collapsed (size 38dp / 2)
private val OpenAccent = 2.5.dp // left accent-bar width when open
private val CollapsedAccent = 2.dp // accent thins into the square

/**
 * Draws the active-row highlight as ONE shape that RESIZES with the rail (no cross-fade of two
 * boxes): a full-width rounded pill with a left accent bar when open, contracting into a fixed
 * [RailAvatarSize] square centered in the [RailCollapsedWidth] band when collapsed. The accent bar
 * is clipped to the pill so its LEFT corners follow the rounding (matching the right) rather than
 * squaring it off. Drawn in the draw phase ([progress] is read here, never at composition) so the
 * morph never recomposes.
 *
 * The collapsed square is a FIXED [RailAvatarSize] and stays vertically centered, so it never
 * distorts when the row grows taller than its band (e.g. a large accessibility font scale) — only
 * the open pill takes the row's full height.
 *
 * [container] and [accent] must be pre-resolved Colors: a @Composable theme token can't be read
 * inside the draw lambda. Pass `active = false` to draw nothing.
 */
fun Modifier.railActiveHighlight(
    active: Boolean,
    progress: () -> Float,
    container: Color,
    accent: Color,
): Modifier = drawBehind {
    if (!active) return@drawBehind
    val p = progress().coerceIn(0f, 1f)
    val insetX = lerp(RailLeftInset.toPx(), OpenInsetX.toPx(), p)
    // Open: a pill of the full row height (− 2·inset). Collapsed: the fixed avatar square. Always
    // vertically centered, so the collapsed square stays square at any row height.
    val rectH = lerp(RailAvatarSize.toPx(), size.height - OpenInsetY.toPx() * 2f, p)
    val top = (size.height - rectH) / 2f
    val r = lerp(CollapsedCorner.toPx(), OpenCorner.toPx(), p)
    val width = size.width - insetX * 2f
    // Container-tint only — no left accent bar (removed in the native-feel pass: Material
    // selection reads as a container fill, not a stripe).
    drawRoundRect(
        color = container,
        topLeft = Offset(insetX, top),
        size = Size(width, rectH),
        cornerRadius = CornerRadius(r, r),
    )
}
