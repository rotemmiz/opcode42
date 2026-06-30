package dev.opcode42.core.design.brand

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.material3.LocalContentColor
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.ClipOp
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.clipPath
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.opcode42.core.design.theme.OnSurfaceFaint

/**
 * The Opcode42 brand mark — a six-point asterisk with a **two-tone dual-arc** center
 * (the "G15² / G15²i" form from the logo board). `42` is ASCII for `*`: static it's
 * the logo, spinning the dual-arc it's the loader. One object, two jobs.
 *
 * Geometry is authored in the design's 160×160 space (center 80,80) and scaled to [size]:
 * three stroked rounded-rect arms (`-7,-51` · `14×102` · `rx 7`, stroke 6) at 0/60/120°,
 * the core punched hollow (r≈15) so the arms don't blob, and a dual-arc ring (r 12,
 * stroke 4, round caps) drawn in **two distinct colors** — the solid top→right arc in
 * [color] and the muted bottom→left arc in [arcColor]. When [spin] is true only the
 * dual-arc rotates (the arms stay put), matching the live loader variant.
 */
@Composable
fun AsteriskMark(
    modifier: Modifier = Modifier,
    size: Dp = 16.dp,
    color: Color = LocalContentColor.current,
    arcColor: Color = OnSurfaceFaint,
    /**
     * Arm stroke in the 160-unit design space. Defaults to the refined 6 of the
     * static G15² mark; the loader (and any small render) wants a heavier stroke so
     * the dual-arc doesn't thin to a sub-pixel sliver — past 7 it switches to the
     * "heavy" form (bigger ring + core punch), matching the design's small-loader.
     */
    strokeWidth: Float = 6f,
    spin: Boolean = false,
    /**
     * Loader "chase" mode: a bright highlight travels around the six arm-halves while the
     * dual-arc spins inside. The geometry is the canonical mark (arms split at the hollow
     * so each can light independently); only the brightness animates, not the shape.
     */
    chase: Boolean = false,
) {
    val heavy = strokeWidth > 7f
    val ringR = if (heavy) 15f else 12f
    val punchR = if (heavy) 18f else 15f
    val arcStrokeWidth = strokeWidth * 2f / 3f
    // The spinning ring's angle is held as State and read *inside* the draw lambda
    // (as late as possible), so an animation frame invalidates only the draw phase —
    // not composition. Several Spinners on screen at 60fps then cost only redraws.
    val angleState: State<Float>? = if (spin || chase) {
        val transition = rememberInfiniteTransition(label = "asterisk")
        transition.animateFloat(
            initialValue = 0f,
            targetValue = 360f,
            animationSpec = infiniteRepeatable(
                animation = tween(durationMillis = 1400, easing = LinearEasing),
                repeatMode = RepeatMode.Restart,
            ),
            label = "ringAngle",
        )
    } else {
        null
    }
    // The chase highlight sweeps the arms a touch faster than the arc rotates.
    val chaseHead: State<Float>? = if (chase) {
        val transition = rememberInfiniteTransition(label = "asterisk-chase")
        transition.animateFloat(
            initialValue = 0f,
            targetValue = 360f,
            animationSpec = infiniteRepeatable(
                animation = tween(durationMillis = 1100, easing = LinearEasing),
                repeatMode = RepeatMode.Restart,
            ),
            label = "chaseHead",
        )
    } else {
        null
    }

    Canvas(modifier.size(size)) {
        val s = this.size.minDimension / 160f
        val center = Offset(80f * s, 80f * s)
        val armStroke = Stroke(width = strokeWidth * s)
        val arcStroke = Stroke(width = arcStrokeWidth * s, cap = StrokeCap.Round)

        // Arms, with the core punched hollow (clip everything OUTSIDE the punch circle).
        val hole = Path().apply { addOval(Rect(center = center, radius = punchR * s)) }
        if (chase) {
            // Six half-arms (the canonical arms split at the hollow) so each can light in
            // turn; the inner ends fall inside the punch, so this renders as the same mark.
            val head = chaseHead?.value ?: 0f
            for (i in 0 until 6) {
                val ang = i * 60f
                clipPath(hole, clipOp = ClipOp.Difference) {
                    rotate(ang, pivot = center) {
                        drawRoundRect(
                            color = color.copy(alpha = chaseAlpha(ang, head)),
                            topLeft = Offset(73f * s, 29f * s),
                            size = Size(14f * s, 51f * s),
                            cornerRadius = CornerRadius(7f * s, 7f * s),
                            style = armStroke,
                        )
                    }
                }
            }
        } else {
            clipPath(hole, clipOp = ClipOp.Difference) {
                for (deg in listOf(0f, 60f, 120f)) {
                    rotate(deg, pivot = center) {
                        drawRoundRect(
                            color = color,
                            topLeft = Offset(73f * s, 29f * s),
                            size = Size(14f * s, 102f * s),
                            cornerRadius = CornerRadius(7f * s, 7f * s),
                            style = armStroke,
                        )
                    }
                }
            }
        }

        // Dual-arc ring in the hollow; only this rotates for the loader.
        val ringTopLeft = Offset((80f - ringR) * s, (80f - ringR) * s)
        val ringSize = Size(2f * ringR * s, 2f * ringR * s)
        rotate(angleState?.value ?: 0f, pivot = center) {
            drawArc(
                color = color,
                startAngle = -90f,
                sweepAngle = 90f,
                useCenter = false,
                topLeft = ringTopLeft,
                size = ringSize,
                style = arcStroke,
            )
            drawArc(
                color = arcColor,
                startAngle = 90f,
                sweepAngle = 90f,
                useCenter = false,
                topLeft = ringTopLeft,
                size = ringSize,
                style = arcStroke,
            )
        }
    }
}

/**
 * Chase brightness for an arm-half at [spokeDeg] given the rotating highlight [headDeg]:
 * brightest where the head sits, easing down to a dim baseline around the ring.
 */
private fun chaseAlpha(spokeDeg: Float, headDeg: Float): Float {
    val d = ((headDeg - spokeDeg) % 360f + 360f) % 360f
    val frac = 1f - d / 360f
    return 0.16f + 0.84f * frac * frac
}
