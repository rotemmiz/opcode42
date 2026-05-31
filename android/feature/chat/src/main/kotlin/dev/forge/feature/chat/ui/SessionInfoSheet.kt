package dev.forge.feature.chat.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.forge.core.model.Session

/**
 * Session-info bottom sheet (design §1 — the `info` action). Surfaces token
 * usage, cost, and code-change summary from the live [Session].
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SessionInfoSheet(session: Session, onDismiss: () -> Unit) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = SurfaceContainerHigh,
    ) {
        Column(modifier = Modifier.fillMaxWidth().padding(start = 20.dp, end = 20.dp, bottom = 28.dp)) {
            Text(
                text = session.title ?: "Session",
                fontSize = 16.sp,
                fontWeight = FontWeight.Medium,
                color = OnSurface,
            )
            session.directory?.let { dir ->
                Text(
                    text = dir,
                    fontFamily = ForgeMono,
                    fontSize = 11.5.sp,
                    color = OnSurfaceFaint,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }

            Spacer(Modifier.padding(top = 16.dp))
            SectionLabel("USAGE")
            val tokens = session.tokens
            if (tokens != null) {
                InfoRow("Input", formatTokenCount(tokens.input))
                InfoRow("Output", formatTokenCount(tokens.output))
                if (tokens.reasoning > 0) InfoRow("Reasoning", formatTokenCount(tokens.reasoning))
                val cacheTotal = tokens.cache.read + tokens.cache.write
                if (cacheTotal > 0) InfoRow("Cache", formatTokenCount(cacheTotal))
                HorizontalDivider(color = Hairline, modifier = Modifier.padding(vertical = 6.dp))
                val total = tokens.input + tokens.output + tokens.reasoning + cacheTotal
                InfoRow("Total tokens", formatTokenCount(total), emphasize = true)
            } else {
                Text("No usage recorded yet.", fontSize = 13.sp, color = OnSurfaceFaint)
            }
            session.cost?.let { cost ->
                InfoRow("Cost", "$" + String.format("%.4f", cost), emphasize = true)
            }

            session.summary?.let { s ->
                if (s.files > 0 || s.additions > 0 || s.deletions > 0) {
                    Spacer(Modifier.padding(top = 16.dp))
                    SectionLabel("CHANGES")
                    InfoRow("Files", s.files.toInt().toString())
                    InfoRow("Lines", "+${s.additions.toInt()} −${s.deletions.toInt()}")
                }
            }
        }
    }
}

@Composable
private fun SectionLabel(text: String) {
    Text(
        text = text,
        fontFamily = ForgeMono,
        fontSize = 11.sp,
        fontWeight = FontWeight.Bold,
        letterSpacing = 1.sp,
        color = HeaderPurple,
        modifier = Modifier.padding(bottom = 8.dp),
    )
}

@Composable
private fun InfoRow(label: String, value: String, emphasize: Boolean = false) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(vertical = 3.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(label, fontSize = 13.5.sp, color = OnSurfaceVariant)
        Text(
            text = value,
            fontFamily = ForgeMono,
            fontSize = 13.sp,
            fontWeight = if (emphasize) FontWeight.Medium else FontWeight.Normal,
            color = if (emphasize) OnSurface else OnSurfaceVariant,
        )
    }
}
