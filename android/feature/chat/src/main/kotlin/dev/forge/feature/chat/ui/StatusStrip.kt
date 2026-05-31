package dev.forge.feature.chat.ui

import androidx.compose.foundation.background
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
import dev.forge.core.model.TokenUsage

/**
 * Compact 32dp mono status strip above the composer (design/android §4).
 * The TUI's bottom status bar, compacted: mode chip · model · provider · tokens.
 */
@Composable
fun StatusStrip(
    mode: String?,
    model: String?,
    provider: String?,
    tokens: TokenUsage?,
    modifier: Modifier = Modifier,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        modifier = modifier
            .fillMaxWidth()
            .height(32.dp)
            .background(Surface)
            .padding(horizontal = 12.dp),
    ) {
        // mode chip — blue fill, 700, 4dp radius
        Text(
            text = (mode ?: "build").replaceFirstChar { it.uppercase() },
            fontFamily = ForgeMono,
            fontSize = 12.sp,
            fontWeight = FontWeight.Bold,
            color = OnPrimary,
            modifier = Modifier
                .clip(ForgeShapes.xs)
                .background(Primary)
                .padding(horizontal = 8.dp, vertical = 1.dp),
        )
        if (model != null) {
            Text(
                text = model,
                fontFamily = ForgeMono,
                fontSize = 12.sp,
                color = OnSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f, fill = false),
            )
        }
        if (provider != null) {
            Text("·", fontFamily = ForgeMono, fontSize = 12.sp, color = OnSurfaceGhost)
            Text(
                text = provider.replaceFirstChar { it.uppercase() },
                fontFamily = ForgeMono,
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
                    text = formatTokenCount(total),
                    fontFamily = ForgeMono,
                    fontSize = 12.sp,
                    color = OnSurfaceFaint,
                )
            }
        }
    }
}

/** Formats a token total compactly: 1234 → "1.2K", 2_400_000 → "2.4M". */
internal fun formatTokenCount(count: Double): String = when {
    count >= 1_000_000 -> String.format("%.1fM", count / 1_000_000)
    count >= 1_000 -> String.format("%.1fK", count / 1_000)
    else -> count.toInt().toString()
}
