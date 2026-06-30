package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.brand.Spinner
import dev.opcode42.core.design.theme.*

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Error
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.model.ToolPart
import dev.opcode42.core.model.ToolStateCompleted
import dev.opcode42.core.model.ToolStateError
import dev.opcode42.core.model.ToolStateRunning

private val TaskResultRegex = Regex("(?s)<task_result>(.*?)</task_result>")

/**
 * Sub-agent block for the `task` tool (design "sub-agent card"): a spark-marked
 * card naming the subagent type + its short description, with a status glyph and
 * the agent's result revealed on tap.
 */
@Composable
fun SubAgentBlock(part: ToolPart, modifier: Modifier = Modifier) {
    var expanded by remember { mutableStateOf(false) }
    val description = part.inputString("description")
    val subagentType = part.inputString("subagent_type", "agent")

    val rawOutput = when (val s = part.state) {
        is ToolStateCompleted -> s.output
        is ToolStateError -> s.error
        else -> null
    }
    val result = rawOutput?.let { out ->
        (TaskResultRegex.find(out)?.groupValues?.get(1) ?: out).trim()
    }?.takeIf { it.isNotEmpty() }

    Column(
        modifier = modifier
            .padding(horizontal = 14.dp, vertical = 4.dp)
            .clip(Opcode42Shapes.sm)
            .background(SurfaceContainer)
            .border(1.dp, OutlineVariant, Opcode42Shapes.sm),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 46.dp)
                .clickable(enabled = result != null) { expanded = !expanded }
                .padding(horizontal = 12.dp, vertical = 10.dp),
        ) {
            Icon(
                Icons.Default.AutoAwesome,
                contentDescription = null,
                tint = HeaderPurple,
                modifier = Modifier.size(16.dp),
            )
            Text(
                text = buildAnnotatedString {
                    withStyle(SpanStyle(color = HeaderPurple, fontWeight = FontWeight.Medium)) {
                        append(subagentType ?: "subagent")
                    }
                    if (description != null) {
                        append("  ")
                        withStyle(SpanStyle(color = OnSurface)) { append(description) }
                    }
                },
                fontSize = 13.sp,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            SubAgentStatus(part)
            if (result != null) {
                Icon(
                    if (expanded) Icons.Default.ExpandLess else Icons.Default.ExpandMore,
                    contentDescription = null,
                    tint = OnSurfaceVariant,
                    modifier = Modifier.size(16.dp),
                )
            }
        }

        if (expanded && result != null) {
            HorizontalDivider(color = Hairline)
            Text(
                text = result,
                fontSize = 13.5.sp,
                lineHeight = 20.sp,
                color = OnSurfaceVariant,
                modifier = Modifier
                    .background(SurfaceContainerLowest)
                    .fillMaxWidth()
                    .padding(12.dp),
            )
        }
    }
}

@Composable
private fun SubAgentStatus(part: ToolPart) {
    when (part.state) {
        is ToolStateRunning -> Spinner(color = Secondary)
        is ToolStateCompleted -> Icon(
            Icons.Default.CheckCircle,
            contentDescription = "done",
            tint = Tertiary,
            // 18dp to match the running Spinner, so the slot doesn't resize when it finishes.
            modifier = Modifier.size(18.dp),
        )
        is ToolStateError -> Icon(
            Icons.Default.Error,
            contentDescription = "error",
            tint = Error,
            modifier = Modifier.size(18.dp),
        )
        else -> Unit
    }
}
