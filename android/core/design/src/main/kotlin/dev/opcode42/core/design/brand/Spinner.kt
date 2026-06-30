package dev.opcode42.core.design.brand

import androidx.compose.material3.LocalContentColor
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.opcode42.core.design.theme.OnSurfaceFaint

/**
 * The brand loader — the [AsteriskMark] with its two-tone dual-arc spinning. This is the
 * one loader used everywhere (session running, in-flight tools, splash), in place of a
 * Material `CircularProgressIndicator`. It stays two-tone (ink + muted), not a single ring.
 */
@Composable
fun Spinner(
    modifier: Modifier = Modifier,
    size: Dp = 16.dp,
    color: Color = LocalContentColor.current,
    arcColor: Color = OnSurfaceFaint,
) {
    AsteriskMark(
        modifier = modifier,
        size = size,
        color = color,
        arcColor = arcColor,
        spin = true,
    )
}
