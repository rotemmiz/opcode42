package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*
import dev.opcode42.core.design.text.StartEllipsisText

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.model.Message
import dev.opcode42.core.model.Part
import dev.opcode42.core.model.TextPart

/**
 * The `/timeline` sheet — lists the session's turns (user + assistant) oldest→newest
 * with a "Revert to here" action per row. Revert undoes a message's effects and
 * restores the prior session state (`POST /session/{id}/revert`, openapi
 * `session.revert`); the server returns the updated [dev.opcode42.core.model.Session],
 * which the caller applies to the store and then reloads the message list.
 *
 * The preview text is the first [TextPart] of each turn (tool calls and patch parts
 * are omitted — the timeline is a turn-level navigator, not a transcript). Rows are
 * disabled while a revert is in flight and a confirmation step guards the action.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TimelineSheet(
    messages: List<Message>,
    onRevert: (messageId: String) -> Unit,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val rows = remember(messages) {
        messages
            .filter { it.role == "user" || it.role == "assistant" }
            .sortedBy { it.time.created }
    }
    var revertingId by remember { mutableStateOf<String?>(null) }
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = SurfaceContainerHigh,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(bottom = 24.dp),
        ) {
            Text(
                text = "Timeline",
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
                color = OnSurface,
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 4.dp),
            )
            Text(
                text = if (rows.isEmpty()) "No turns yet." else "${rows.size} ${if (rows.size == 1) "turn" else "turns"}",
                fontFamily = Opcode42Mono,
                fontSize = 11.5.sp,
                color = OnSurfaceFaint,
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 10.dp),
            )
            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 520.dp)
                    .padding(horizontal = 8.dp),
            ) {
                items(rows, key = { it.id }) { msg ->
                    TimelineRow(
                        message = msg,
                        busy = revertingId != null,
                        onRevert = {
                            revertingId = msg.id
                            onRevert(msg.id)
                        },
                    )
                    HorizontalDivider(color = Hairline)
                }
            }
        }
    }
}

@Composable
private fun TimelineRow(
    message: Message,
    busy: Boolean,
    onRevert: () -> Unit,
) {
    val preview = remember(message.parts) { previewText(message.parts) }
    val isUser = message.role == "user"
    Row(
        verticalAlignment = Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 52.dp)
            .padding(horizontal = 10.dp, vertical = 8.dp),
    ) {
        Column(
            modifier = Modifier
                .weight(1f)
                .heightIn(min = 36.dp),
        ) {
            Text(
                text = buildAnnotatedString {
                    withStyle(
                        SpanStyle(
                            color = if (isUser) Secondary else HeaderPurple,
                            fontWeight = FontWeight.Bold,
                        ),
                    ) { append(if (isUser) "YOU" else "AGENT") }
                },
                fontFamily = Opcode42Mono,
                fontSize = 11.sp,
            )
            Spacer(Modifier.size(2.dp))
            if (preview.isNotBlank()) {
                StartEllipsisText(
                    text = preview,
                    style = TextStyle(
                        fontFamily = Opcode42Mono,
                        fontSize = 13.sp,
                        color = OnSurface,
                    ),
                    modifier = Modifier.fillMaxWidth(),
                )
            } else {
                Text(
                    text = "(no text)",
                    fontFamily = Opcode42Mono,
                    fontSize = 13.sp,
                    color = OnSurfaceFaint,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        Text(
            text = "Revert to here",
            fontSize = 13.sp,
            fontWeight = FontWeight.Medium,
            color = if (busy) OnSurfaceFaint else LinkCyan,
            modifier = Modifier
                .clip(RoundedCornerShape(6.dp))
                .then(if (busy) Modifier else Modifier.clickable(onClick = onRevert))
                .padding(horizontal = 10.dp, vertical = 6.dp),
        )
    }
}

private fun previewText(parts: List<Part>): String =
    parts.filterIsInstance<TextPart>().joinToString(" ") { it.text }.trim()