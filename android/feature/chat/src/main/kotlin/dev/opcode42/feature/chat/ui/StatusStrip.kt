package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.format.formatCompactCount
import dev.opcode42.core.design.theme.*

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.model.TokenUsage

/**
 * 40dp status strip above the composer (mobile.md §1) on an elevated surface.
 * The TUI's bottom status bar, compacted: mode chip · model · provider · tokens.
 */
@Composable
fun StatusStrip(
    mode: String?,
    model: String?,
    provider: String?,
    tokens: TokenUsage?,
    status: String? = null,
    retryAttempt: Int? = null,
    modifier: Modifier = Modifier,
    onClick: (() -> Unit)? = null,
    onModeClick: (() -> Unit)? = null,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        modifier = modifier
            .fillMaxWidth()
            .height(40.dp)
            .background(SurfaceContainerHigh)
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(horizontal = 12.dp),
    ) {
        // mode chip — blue fill, 700, 4dp radius. Its own tap opens the agent picker.
        Text(
            text = (mode ?: "build").replaceFirstChar { it.uppercase() },
            fontFamily = Opcode42Mono,
            fontSize = 12.sp,
            fontWeight = FontWeight.Bold,
            color = OnPrimary,
            modifier = Modifier
                .clip(Opcode42Shapes.xs)
                .background(Primary)
                .then(if (onModeClick != null) Modifier.clickable(onClick = onModeClick) else Modifier)
                .padding(horizontal = 8.dp, vertical = 1.dp),
        )
        // retry chip — surfaces `session.status` type "retry" (WS I5 E19). The opencode TUI
        // renders this as "Retrying (attempt N)" (tui/src/routes/session/index.tsx:2316);
        // we match that wording. Filled with Secondary so it reads as an active/recovering
        // state distinct from the idle mode chip. Omitted entirely when not retrying.
        retryStatusLabel(status, retryAttempt)?.let { label ->
            Text(
                text = label,
                fontFamily = Opcode42Mono,
                fontSize = 12.sp,
                fontWeight = FontWeight.Bold,
                color = OnSecondary,
                modifier = Modifier
                    .clip(Opcode42Shapes.xs)
                    .background(Secondary)
                    .padding(horizontal = 8.dp, vertical = 1.dp),
            )
        }
        if (model != null) {
            Text(
                text = model,
                fontFamily = Opcode42Mono,
                fontSize = 12.sp,
                color = OnSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f, fill = false),
            )
        }
        if (provider != null) {
            Text("·", fontFamily = Opcode42Mono, fontSize = 12.sp, color = OnSurfaceGhost)
            Text(
                text = provider,
                fontFamily = Opcode42Mono,
                fontSize = 12.sp,
                color = OnSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        // push the token count to the right edge (mock's margin-left:auto)
        Spacer(Modifier.weight(1f))
        tokens?.let {
            val total = it.input + it.output + it.reasoning + it.cache.read + it.cache.write
            if (total > 0) {
                Text(
                    text = formatCompactCount(total),
                    fontFamily = Opcode42Mono,
                    fontSize = 12.sp,
                    color = OnSurfaceFaint,
                )
            }
        }
    }
}

/**
 * The retry chip's label for the [StatusStrip], or null when the session is not retrying.
 *
 * opencode's `session.status` event carries a nested `status` object whose `type` may be
 * `"retry"` (openapi.json SessionStatus schema, `packages/sdk/openapi.json:17278-17324`).
 * The retry variant requires an integer `attempt` field (no max is defined on the wire, so
 * we show the attempt count alone, never a fabricated "N/M"). The opencode TUI renders this
 * as `"Retrying (attempt ${attempt})"` (`packages/tui/src/routes/session/index.tsx:2316`),
 * with no lower-bound guard — we match that wording exactly, including attempt 0. When
 * `attempt` is absent/blank we fall back to bare "Retrying" — the plan's "if the event
 * carries an attempt count" guard against fabricating a count.
 */
fun retryStatusLabel(status: String?, retryAttempt: Int?): String? {
    if (status != "retry") return null
    return retryAttempt?.let { "Retrying (attempt $it)" } ?: "Retrying"
}
