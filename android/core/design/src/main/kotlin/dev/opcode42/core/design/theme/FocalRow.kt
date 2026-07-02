package dev.opcode42.core.design.theme

import androidx.compose.foundation.background
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier

/**
 * The app's canonical selected/active-row treatment — a Material 3 selection affordance:
 * `secondaryContainer` amber tint fill, **no left accent bar** (the left bar was removed in
 * the native-feel pass per the UX review — Material selection reads as a container tint,
 * not a stripe). Used by the active diff-card header, session rows, and the composer sheets
 * (command palette, @-mention, model and agent pickers) so selection reads identically
 * everywhere.
 *
 * When [active] is false this is a no-op, so call sites can apply it unconditionally:
 * `Modifier.focalRow(active = selected)`.
 */
@Composable
fun Modifier.focalRow(active: Boolean): Modifier {
    if (!active) return this
    return this.background(SecondaryContainer)
}
