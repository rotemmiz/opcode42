package dev.forge.feature.chat.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color

/**
 * Forge's named design tokens (design/android/tokens.css), resolved through
 * [LocalForgeColors] so the whole stream re-skins when the theme flips. The six
 * semantic hues keep their meaning in both schemes; the surface ramp and
 * on-colors invert for the light scheme.
 */
data class ForgeColors(
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
val DarkForgeColors = ForgeColors(
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
val LightForgeColors = ForgeColors(
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

val LocalForgeColors = staticCompositionLocalOf { DarkForgeColors }

// ─── Token accessors — read the active scheme from the composition local. ──────
val Surface: Color @Composable get() = LocalForgeColors.current.surface
val SurfaceContainerLowest: Color @Composable get() = LocalForgeColors.current.surfaceContainerLowest
val SurfaceContainerLow: Color @Composable get() = LocalForgeColors.current.surfaceContainerLow
val SurfaceContainer: Color @Composable get() = LocalForgeColors.current.surfaceContainer
val SurfaceContainerHigh: Color @Composable get() = LocalForgeColors.current.surfaceContainerHigh
val SurfaceContainerHighest: Color @Composable get() = LocalForgeColors.current.surfaceContainerHighest
val OutlineVariant: Color @Composable get() = LocalForgeColors.current.outlineVariant
val Hairline: Color @Composable get() = LocalForgeColors.current.hairline
val Outline: Color @Composable get() = LocalForgeColors.current.outline
val OnSurface: Color @Composable get() = LocalForgeColors.current.onSurface
val OnSurfaceVariant: Color @Composable get() = LocalForgeColors.current.onSurfaceVariant
val OnSurfaceFaint: Color @Composable get() = LocalForgeColors.current.onSurfaceFaint
val OnSurfaceGhost: Color @Composable get() = LocalForgeColors.current.onSurfaceGhost
val Primary: Color @Composable get() = LocalForgeColors.current.primary
val Secondary: Color @Composable get() = LocalForgeColors.current.secondary
val Tertiary: Color @Composable get() = LocalForgeColors.current.tertiary
val Error: Color @Composable get() = LocalForgeColors.current.error
val HeaderPurple: Color @Composable get() = LocalForgeColors.current.headerPurple
val LinkCyan: Color @Composable get() = LocalForgeColors.current.linkCyan
val OnPrimary: Color @Composable get() = LocalForgeColors.current.onPrimary
val OnSecondary: Color @Composable get() = LocalForgeColors.current.onSecondary

private fun ForgeColors.toM3Scheme(dark: Boolean) = if (dark) {
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
fun ForgeTheme(
    darkTheme: Boolean = true,
    content: @Composable () -> Unit,
) {
    val colors = if (darkTheme) DarkForgeColors else LightForgeColors
    CompositionLocalProvider(LocalForgeColors provides colors) {
        MaterialTheme(
            colorScheme = colors.toM3Scheme(darkTheme),
            content = content,
        )
    }
}
