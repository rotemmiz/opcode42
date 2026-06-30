package dev.opcode42.core.design.theme

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import dev.opcode42.core.design.R

/**
 * Roboto Mono — the design's typeface for all code/diff/tool/status/mono text.
 * The platform `FontFamily.Monospace` maps to Droid Sans Mono on Android, not
 * Roboto Mono, so we bundle it (res/font).
 */
val Opcode42Mono = FontFamily(
    Font(R.font.roboto_mono_regular, FontWeight.Normal),
    Font(R.font.roboto_mono_medium, FontWeight.Medium),
    Font(R.font.roboto_mono_bold, FontWeight.Bold),
)

/**
 * Opcode42's tighter-than-stock shape scale (design tokens.css):
 * r-xs 4dp (code/diff blocks, chip, status pills) · r-sm 8dp (tool/diff cards) ·
 * r-md 12dp (message/system cards) · r-lg 16dp (bottom sheets).
 */
object Opcode42Shapes {
    val xs = RoundedCornerShape(4.dp)
    val sm = RoundedCornerShape(8.dp)
    val md = RoundedCornerShape(12.dp)
    val lg = RoundedCornerShape(16.dp)
}

/**
 * Opcode42's named design tokens (design/android/tokens.css), resolved through
 * [LocalOpcode42Colors] so the whole stream re-skins when the theme flips. The six
 * semantic hues keep their meaning in both schemes; the surface ramp and
 * on-colors invert for the light scheme.
 */
data class Opcode42Colors(
    val surface: Color,
    val surfaceContainerLowest: Color,
    val surfaceContainerLow: Color,
    val surfaceContainer: Color,
    val surfaceContainerHigh: Color,
    val surfaceContainerHighest: Color,
    val outlineVariant: Color,
    val hairline: Color,
    val outline: Color,
    val onSurface: Color,
    val onSurfaceVariant: Color,
    val onSurfaceFaint: Color,
    val onSurfaceGhost: Color,
    val primary: Color,
    val secondary: Color,
    val tertiary: Color,
    val error: Color,
    val headerPurple: Color,
    val linkCyan: Color,
    val onPrimary: Color,
    val onSecondary: Color,
)

// ─── Dark scheme (the charcoal default) ───────────────────────────────────────
val DarkOpcode42Colors = Opcode42Colors(
    surface = Color(0xFF15171A),
    surfaceContainerLowest = Color(0xFF101316),
    surfaceContainerLow = Color(0xFF181B1F),
    surfaceContainer = Color(0xFF1C1F23),
    surfaceContainerHigh = Color(0xFF20242A),
    surfaceContainerHighest = Color(0xFF262B31),
    outlineVariant = Color(0xFF2C3137),
    hairline = Color(0xFF23272C),
    outline = Color(0xFF3A4047),
    onSurface = Color(0xFFD6DADE),
    onSurfaceVariant = Color(0xFF8B929A),
    onSurfaceFaint = Color(0xFF585F67),
    onSurfaceGhost = Color(0xFF3A4047),
    primary = Color(0xFF6FA8DC),
    secondary = Color(0xFFD99A4E),
    tertiary = Color(0xFF8CC265),
    error = Color(0xFFE0606E),
    headerPurple = Color(0xFFB08CD4),
    linkCyan = Color(0xFF5FB3C4),
    onPrimary = Color(0xFF0A1722),
    onSecondary = Color(0xFF1A1207),
)

// ─── Light scheme (inverted ramp; hues darkened for AA on light surfaces) ──────
val LightOpcode42Colors = Opcode42Colors(
    surface = Color(0xFFFCFBF9),
    surfaceContainerLowest = Color(0xFFF1EEEA),
    surfaceContainerLow = Color(0xFFF6F3F0),
    surfaceContainer = Color(0xFFF0EDE9),
    surfaceContainerHigh = Color(0xFFE9E5E0),
    surfaceContainerHighest = Color(0xFFE1DCD6),
    outlineVariant = Color(0xFFD2CCC4),
    hairline = Color(0xFFE4DFD9),
    outline = Color(0xFFADA59B),
    onSurface = Color(0xFF201D19),
    onSurfaceVariant = Color(0xFF57514A),
    onSurfaceFaint = Color(0xFF8B837A),
    onSurfaceGhost = Color(0xFFBCB4AA),
    primary = Color(0xFF2D6CB2),
    secondary = Color(0xFFA66E16),
    tertiary = Color(0xFF3C7B2C),
    error = Color(0xFFBF3446),
    headerPurple = Color(0xFF7C57AE),
    linkCyan = Color(0xFF1E8092),
    onPrimary = Color(0xFFFFFFFF),
    onSecondary = Color(0xFFFFFFFF),
)

val LocalOpcode42Colors = staticCompositionLocalOf { DarkOpcode42Colors }

// ─── Token accessors — read the active scheme from the composition local. ──────
val Surface: Color @Composable get() = LocalOpcode42Colors.current.surface
val SurfaceContainerLowest: Color @Composable get() = LocalOpcode42Colors.current.surfaceContainerLowest
val SurfaceContainerLow: Color @Composable get() = LocalOpcode42Colors.current.surfaceContainerLow
val SurfaceContainer: Color @Composable get() = LocalOpcode42Colors.current.surfaceContainer
val SurfaceContainerHigh: Color @Composable get() = LocalOpcode42Colors.current.surfaceContainerHigh
val SurfaceContainerHighest: Color @Composable get() = LocalOpcode42Colors.current.surfaceContainerHighest
val OutlineVariant: Color @Composable get() = LocalOpcode42Colors.current.outlineVariant
val Hairline: Color @Composable get() = LocalOpcode42Colors.current.hairline
val Outline: Color @Composable get() = LocalOpcode42Colors.current.outline
val OnSurface: Color @Composable get() = LocalOpcode42Colors.current.onSurface
val OnSurfaceVariant: Color @Composable get() = LocalOpcode42Colors.current.onSurfaceVariant
val OnSurfaceFaint: Color @Composable get() = LocalOpcode42Colors.current.onSurfaceFaint
val OnSurfaceGhost: Color @Composable get() = LocalOpcode42Colors.current.onSurfaceGhost
val Primary: Color @Composable get() = LocalOpcode42Colors.current.primary
val Secondary: Color @Composable get() = LocalOpcode42Colors.current.secondary
val Tertiary: Color @Composable get() = LocalOpcode42Colors.current.tertiary
val Error: Color @Composable get() = LocalOpcode42Colors.current.error
val HeaderPurple: Color @Composable get() = LocalOpcode42Colors.current.headerPurple
val LinkCyan: Color @Composable get() = LocalOpcode42Colors.current.linkCyan
val OnPrimary: Color @Composable get() = LocalOpcode42Colors.current.onPrimary
val OnSecondary: Color @Composable get() = LocalOpcode42Colors.current.onSecondary

private fun Opcode42Colors.toM3Scheme(dark: Boolean) = if (dark) {
    darkColorScheme(
        background = surface, surface = surface,
        surfaceContainerLowest = surfaceContainerLowest,
        surfaceContainerLow = surfaceContainerLow,
        surfaceContainer = surfaceContainer,
        surfaceContainerHigh = surfaceContainerHigh,
        surfaceContainerHighest = surfaceContainerHighest,
        onBackground = onSurface, onSurface = onSurface, onSurfaceVariant = onSurfaceVariant,
        outline = outline, outlineVariant = outlineVariant,
        primary = primary, onPrimary = onPrimary,
        secondary = secondary, onSecondary = onSecondary,
        tertiary = tertiary, error = error,
    )
} else {
    lightColorScheme(
        background = surface, surface = surface,
        surfaceContainerLowest = surfaceContainerLowest,
        surfaceContainerLow = surfaceContainerLow,
        surfaceContainer = surfaceContainer,
        surfaceContainerHigh = surfaceContainerHigh,
        surfaceContainerHighest = surfaceContainerHighest,
        onBackground = onSurface, onSurface = onSurface, onSurfaceVariant = onSurfaceVariant,
        outline = outline, outlineVariant = outlineVariant,
        primary = primary, onPrimary = onPrimary,
        secondary = secondary, onSecondary = onSecondary,
        tertiary = tertiary, error = error,
    )
}

@Composable
fun Opcode42Theme(
    darkTheme: Boolean = true,
    content: @Composable () -> Unit,
) {
    val colors = if (darkTheme) DarkOpcode42Colors else LightOpcode42Colors
    CompositionLocalProvider(LocalOpcode42Colors provides colors) {
        MaterialTheme(
            colorScheme = colors.toM3Scheme(darkTheme),
            content = content,
        )
    }
}
