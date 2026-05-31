package dev.forge.feature.chat.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

// ─── Design tokens from design/android/tokens.css ─────────────────────────────
// Surface ramp — dark scheme
val Surface = Color(0xFF15171A)
val SurfaceContainerLowest = Color(0xFF101316)
val SurfaceContainerLow = Color(0xFF181B1F)
val SurfaceContainer = Color(0xFF1C1F23)
val SurfaceContainerHigh = Color(0xFF20242A)
val SurfaceContainerHighest = Color(0xFF262B31)
val OutlineVariant = Color(0xFF2C3137)
val Hairline = Color(0xFF23272C)
val Outline = Color(0xFF3A4047)

// Text
val OnSurface = Color(0xFFD6DADE)
val OnSurfaceVariant = Color(0xFF8B929A)
val OnSurfaceFaint = Color(0xFF585F67)
val OnSurfaceGhost = Color(0xFF3A4047)

// Semantic colors
val Primary = Color(0xFF6FA8DC)       // blue — agent mode, send button
val Secondary = Color(0xFFD99A4E)     // amber — active state, in-progress
val Tertiary = Color(0xFF8CC265)      // green — success, added lines
val Error = Color(0xFFE0606E)         // red — errors, removed lines
val HeaderPurple = Color(0xFFB08CD4)  // purple — section headers
val LinkCyan = Color(0xFF5FB3C4)      // cyan — mentions, links

val OnPrimary = Color(0xFF0A1722)
val OnSecondary = Color(0xFF1A1207)

val DarkScheme = darkColorScheme(
    background = Surface,
    surface = Surface,
    surfaceContainerLowest = SurfaceContainerLowest,
    surfaceContainerLow = SurfaceContainerLow,
    surfaceContainer = SurfaceContainer,
    surfaceContainerHigh = SurfaceContainerHigh,
    surfaceContainerHighest = SurfaceContainerHighest,
    onBackground = OnSurface,
    onSurface = OnSurface,
    onSurfaceVariant = OnSurfaceVariant,
    outline = Outline,
    outlineVariant = OutlineVariant,
    primary = Primary,
    onPrimary = OnPrimary,
    secondary = Secondary,
    onSecondary = OnSecondary,
    tertiary = Tertiary,
    error = Error,
)

val LightScheme = lightColorScheme(
    primary = Primary,
    secondary = Secondary,
    tertiary = Tertiary,
    error = Error,
)

@Composable
fun ForgeTheme(
    darkTheme: Boolean = true,
    content: @Composable () -> Unit,
) {
    MaterialTheme(
        colorScheme = if (darkTheme) DarkScheme else LightScheme,
        content = content,
    )
}
