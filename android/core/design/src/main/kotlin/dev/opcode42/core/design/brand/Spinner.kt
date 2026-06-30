package dev.opcode42.core.design.brand

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.MutableTransitionState
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.material3.LocalContentColor
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.opcode42.core.design.theme.OnSurfaceFaint

/**
 * The brand loader — the [AsteriskMark] in "chase" mode: a bright highlight travels around
 * the arms while the two-tone dual-arc spins inside. This is the one loader used everywhere
 * (session running, in-flight tools, full-screen load), in place of a Material
 * `CircularProgressIndicator`, at a single consistent [size].
 *
 * It fades itself in (alpha 0→1) when it first appears and out (1→0) when [visible] flips
 * false, so it never pops. Pass `visible` from the show/hide condition and always compose
 * the Spinner (rather than guarding it with `if`) to get the fade-out too.
 */
@Composable
fun Spinner(
    modifier: Modifier = Modifier,
    size: Dp = 18.dp,
    color: Color = LocalContentColor.current,
    arcColor: Color = OnSurfaceFaint,
    visible: Boolean = true,
) {
    // Seed the transition at "hidden" then flip to [visible]: even an initially-visible
    // spinner animates in from 0, and a later visible=false animates out before it leaves.
    val state = remember { MutableTransitionState(false) }
    state.targetState = visible
    AnimatedVisibility(
        visibleState = state,
        modifier = modifier,
        enter = fadeIn(tween(200)),
        exit = fadeOut(tween(200)),
    ) {
        AsteriskMark(
            size = size,
            color = color,
            arcColor = arcColor,
            strokeWidth = 6f,
            chase = true,
        )
    }
}
