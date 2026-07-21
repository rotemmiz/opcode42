package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.brand.Spinner
import dev.opcode42.core.design.theme.*

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.OpenInNew
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Error
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.model.Message
import dev.opcode42.core.model.ToolPart
import dev.opcode42.core.model.ToolStateCompleted
import dev.opcode42.core.model.ToolStateError
import dev.opcode42.core.model.ToolStateRunning
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive

private val TaskResultRegex = Regex("(?s)<task_result>(.*?)</task_result>")
private val TaskIdRegex = Regex("<task id=\"([^\"]+)\"")

/**
 * Extracts the child session id spawned by the `task` tool, in priority order:
 * 1. `metadata.sessionId` on the tool state (set by opencode's TaskTool).
 * 2. The `id` attribute of the `<task id="..." state="...">` wrapper in the output text.
 */
internal fun ToolPart.childSessionId(): String? {
    val meta = when (val s = state) {
        is ToolStateRunning -> s.metadata
        is ToolStateCompleted -> s.metadata
        is ToolStateError -> s.metadata
        else -> null
    }
    meta?.get("sessionId")?.jsonPrimitive?.content?.takeIf { it.isNotBlank() }?.let { return it }
    val output = (state as? ToolStateCompleted)?.output
        ?: (state as? ToolStateError)?.error
    return output?.let { TaskIdRegex.find(it)?.groupValues?.get(1) }
}

/**
 * D1 — Sub-agent block for the `task` tool: a tappable card naming the subagent type +
 * its short description, with a status glyph. Expanding the card loads and shows the
 * child session's transcript inline (via [onLoadChild] + [childMessages]). An "Open in
 * new view" affordance navigates to the child session as its own chat ([onNavigateToSession]).
 *
 * While the subagent is running, a live spinner reflects the tool state. When it
 * completes, the card auto-expands to show the result summary + the transcript.
 */
@Composable
fun SubAgentBlock(
    part: ToolPart,
    modifier: Modifier = Modifier,
    childMessages: List<Message> = emptyList(),
    onLoadChild: (String) -> Unit = {},
    onNavigateToSession: (String) -> Unit = {},
) {
    var expanded by remember { mutableStateOf(false) }
    var loaded by remember { mutableStateOf(false) }
    val description = part.inputString("description")
    val subagentType = part.inputString("subagent_type", "agent")
    val childId = remember(part) { part.childSessionId() }

    val rawOutput = when (val s = part.state) {
        is ToolStateCompleted -> s.output
        is ToolStateError -> s.error
        else -> null
    }
    val result = rawOutput?.let { out ->
        (TaskResultRegex.find(out)?.groupValues?.get(1) ?: out).trim()
    }?.takeIf { it.isNotEmpty() }

    val isRunning = part.state is ToolStateRunning

    // Auto-expand on completion if we have a child session id, so the user sees the result.
    LaunchedEffect(part.state) {
        if (part.state is ToolStateCompleted && childId != null && !loaded) {
            expanded = true
            loaded = true
            onLoadChild(childId)
        }
    }

    // Load the child transcript on first expand.
    LaunchedEffect(expanded, childId) {
        if (expanded && childId != null && !loaded) {
            loaded = true
            onLoadChild(childId)
        }
    }

    CollapsibleToolCard(
        expanded = expanded,
        onToggle = { expanded = !expanded },
        modifier = modifier,
        leading = {
            Icon(
                Icons.Default.AutoAwesome,
                contentDescription = null,
                tint = HeaderPurple,
                modifier = Modifier.size(16.dp),
            )
        },
        title = {
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
        },
        trailing = {
            SubAgentStatus(part)
        },
    ) {
        // The child session's transcript (loaded on first expand). If the child id
        // is unknown or the load hasn't completed, fall back to the task_result text.
        if (childId != null && childMessages.isNotEmpty()) {
            ChildTranscript(
                messages = childMessages,
                onNavigateToSession = { onNavigateToSession(childId) },
                modifier = Modifier.fillMaxWidth(),
            )
        } else if (result != null) {
            Text(
                text = result,
                style = Opcode42Typography.bodySmall,
                color = OnSurfaceVariant,
                modifier = Modifier
                    .background(SurfaceContainerLowest)
                    .fillMaxWidth()
                    .padding(12.dp),
            )
        } else if (isRunning) {
            Text(
                text = "Subagent is working…",
                style = Opcode42Typography.bodySmall,
                color = OnSurfaceVariant,
                modifier = Modifier.padding(12.dp),
            )
        }

        // "Open in new view" — navigate to the child session as its own chat.
        if (childId != null) {
            Row(
                modifier = Modifier.fillMaxWidth().padding(8.dp),
                horizontalArrangement = Arrangement.End,
            ) {
                OutlinedButton(
                    onClick = { onNavigateToSession(childId) },
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(
                        horizontal = 12.dp, vertical = 4.dp,
                    ),
                ) {
                    Icon(
                        Icons.AutoMirrored.Filled.OpenInNew,
                        contentDescription = null,
                        modifier = Modifier.size(14.dp),
                    )
                    Spacer(Modifier.size(4.dp))
                    Text("Open in new view", fontSize = 12.sp)
                }
            }
        }
    }
}

@Composable
private fun ChildTranscript(
    messages: List<Message>,
    onNavigateToSession: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .heightIn(max = 320.dp)
            .background(SurfaceContainerLowest),
    ) {
        // Newest-first like the main chat; cap height so it scrolls within the card.
        LazyColumn(
            modifier = Modifier.fillMaxWidth(),
            reverseLayout = true,
        ) {
            items(messages.asReversed(), key = { it.id }) { msg ->
                val text = msg.parts
                    .filterIsInstance<dev.opcode42.core.model.TextPart>()
                    .joinToString("\n") { it.text }
                    .takeIf { it.isNotBlank() }
                    ?: "(no text)"
                Text(
                    text = buildAnnotatedString {
                        withStyle(SpanStyle(color = OnSurfaceVariant, fontSize = 11.sp)) {
                            append("[${msg.role}]")
                        }
                        append(" ")
                        withStyle(SpanStyle(color = OnSurface, fontSize = 12.sp)) {
                            append(text)
                        }
                    },
                    style = Opcode42Typography.bodySmall,
                    color = OnSurface,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp),
                )
            }
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