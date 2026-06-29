package dev.opcode42.feature.chat.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.model.ToolPart
import dev.opcode42.core.model.ToolStateCompleted
import dev.opcode42.core.model.ToolStateError
import dev.opcode42.core.model.ToolStateRunning

/**
 * Dedicated collapsible block for tools whose output/content is the point:
 *   bash  → `$ <command>` header, captured stdout/stderr body
 *   write → `Write <filename>` header, the written file content body
 * (design §2 "Bash output" / "Write/code listing" block kinds).
 */
/** When a tool log is opened it shows at most this many lines; the rest is behind "Show all". */
private const val TOOL_LOG_PREVIEW_LINES = 7

@Composable
fun ToolOutputBlock(part: ToolPart, modifier: Modifier = Modifier) {
    var expanded by remember { mutableStateOf(false) }
    val isBash = part.tool.lowercase() == "bash"
    val isError = part.state is ToolStateError

    val command = part.inputString("command")
    val filename = part.inputString("filePath", "path")?.substringAfterLast('/')
    val body = if (isBash) {
        when (val s = part.state) {
            is ToolStateCompleted -> s.output
            is ToolStateError -> s.error
            is ToolStateRunning -> s.title
            else -> null
        }
    } else {
        part.inputString("content")
    }
    val lineCount = body?.trim()?.takeIf { it.isNotEmpty() }?.lines()?.size

    Column(
        modifier = modifier
            .padding(horizontal = 14.dp, vertical = 4.dp)
            .clip(Opcode42Shapes.sm)
            .background(SurfaceContainer)
            .border(1.dp, OutlineVariant, Opcode42Shapes.sm),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 46.dp)
                .clickable { expanded = !expanded }
                .padding(horizontal = 12.dp, vertical = 10.dp),
        ) {
            Icon(
                if (expanded) Icons.Default.ExpandLess else Icons.Default.ExpandMore,
                contentDescription = null,
                tint = OnSurfaceVariant,
                modifier = Modifier.size(16.dp),
            )
            Spacer(Modifier.width(6.dp))
            Text(
                text = buildAnnotatedString {
                    if (isBash) {
                        withStyle(SpanStyle(color = OnSurfaceFaint)) { append("$ ") }
                        withStyle(SpanStyle(color = OnSurface)) { append(command ?: "bash") }
                    } else {
                        withStyle(SpanStyle(color = OnSurface)) { append("Write ") }
                        withStyle(SpanStyle(color = Tertiary)) { append(filename ?: "file") }
                    }
                },
                fontFamily = Opcode42Mono,
                fontSize = 13.sp,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            if (isError) {
                Text("error", fontFamily = Opcode42Mono, fontSize = 11.sp, color = Error)
            } else if (lineCount != null) {
                Text(
                    text = if (lineCount == 1) "1 line" else "$lineCount lines",
                    fontFamily = Opcode42Mono,
                    fontSize = 11.sp,
                    color = OnSurfaceFaint,
                )
            }
        }

        if (expanded && !body.isNullOrEmpty()) {
            HorizontalDivider(color = Hairline)
            // Cap the opened log to a short preview; reset to capped each time the block
            // is opened (remember keyed on `expanded`) so a long log never floods the stream.
            // Trim so the cap counts the same lines as the header badge (`lineCount`),
            // otherwise a trailing newline makes the badge say "7 lines" while the button
            // says "Show all 8 lines" and reveals only a blank trailing line.
            val lines = remember(body) { body.trim().lines() }
            var showAll by remember(expanded) { mutableStateOf(false) }
            val capped = lines.size > TOOL_LOG_PREVIEW_LINES
            val shown = if (capped && !showAll) {
                lines.take(TOOL_LOG_PREVIEW_LINES).joinToString("\n")
            } else {
                lines.joinToString("\n")
            }
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(SurfaceContainerLowest)
                    .horizontalScroll(rememberScrollState()),
            ) {
                Text(
                    text = shown,
                    fontFamily = Opcode42Mono,
                    fontSize = 12.sp,
                    lineHeight = 18.sp,
                    color = if (isError) Error else OnSurfaceVariant,
                    softWrap = false,
                    modifier = Modifier.padding(12.dp),
                )
            }
            if (capped) {
                HorizontalDivider(color = Hairline)
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { showAll = !showAll }
                        .padding(horizontal = 12.dp, vertical = 8.dp),
                ) {
                    Icon(
                        if (showAll) Icons.Default.ExpandLess else Icons.Default.ExpandMore,
                        contentDescription = null,
                        tint = Secondary,
                        modifier = Modifier.size(16.dp),
                    )
                    Spacer(Modifier.width(6.dp))
                    Text(
                        text = if (showAll) {
                            "Show less"
                        } else {
                            "Show all ${lines.size} lines"
                        },
                        fontFamily = Opcode42Mono,
                        fontSize = 12.sp,
                        color = Secondary,
                    )
                }
            }
        }
    }
}
