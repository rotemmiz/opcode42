package dev.forge.feature.chat.ui

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
import dev.forge.core.model.ToolPart
import dev.forge.core.model.ToolStateCompleted
import dev.forge.core.model.ToolStateError
import dev.forge.core.model.ToolStateRunning

/**
 * Dedicated collapsible block for tools whose output/content is the point:
 *   bash  → `$ <command>` header, captured stdout/stderr body
 *   write → `Write <filename>` header, the written file content body
 * (design §2 "Bash output" / "Write/code listing" block kinds).
 */
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
            .clip(ForgeShapes.sm)
            .background(SurfaceContainer)
            .border(1.dp, OutlineVariant, ForgeShapes.sm),
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
                fontFamily = ForgeMono,
                fontSize = 13.sp,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            if (isError) {
                Text("error", fontFamily = ForgeMono, fontSize = 11.sp, color = Error)
            } else if (lineCount != null) {
                Text(
                    text = if (lineCount == 1) "1 line" else "$lineCount lines",
                    fontFamily = ForgeMono,
                    fontSize = 11.sp,
                    color = OnSurfaceFaint,
                )
            }
        }

        if (expanded && !body.isNullOrEmpty()) {
            HorizontalDivider(color = Hairline)
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(SurfaceContainerLowest)
                    .horizontalScroll(rememberScrollState()),
            ) {
                Text(
                    text = body,
                    fontFamily = ForgeMono,
                    fontSize = 12.sp,
                    lineHeight = 18.sp,
                    color = if (isError) Error else OnSurfaceVariant,
                    softWrap = false,
                    modifier = Modifier.padding(12.dp),
                )
            }
        }
    }
}
