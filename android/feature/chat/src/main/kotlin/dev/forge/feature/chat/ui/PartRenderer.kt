package dev.forge.feature.chat.ui

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.forge.core.model.*

/**
 * Renders a single Part.
 * Design follows design/android/README.md — Terminal-Material direction.
 */
@Composable
fun PartRenderer(part: Part, modifier: Modifier = Modifier) {
    when (part) {
        is TextPart -> TextPartView(part, modifier)
        is ReasoningPart -> ReasoningPartView(part, modifier)
        is ToolPart -> ToolPartView(part, modifier)
        is FilePart -> FilePartView(part, modifier)
        is StepStartPart, is StepFinishPart -> Unit  // invisible separators
        is UnknownPart -> Unit
    }
}

// ─── Text ─────────────────────────────────────────────────────────────────────

@Composable
private fun TextPartView(part: TextPart, modifier: Modifier = Modifier) {
    if (part.text.isBlank()) return
    Text(
        text = part.text,
        style = MaterialTheme.typography.bodyMedium.copy(
            fontSize = 14.5.sp,
            lineHeight = 22.sp,
        ),
        color = OnSurface,
        modifier = modifier.padding(horizontal = 14.dp, vertical = 4.dp),
    )
}

// ─── Reasoning ────────────────────────────────────────────────────────────────

@Composable
private fun ReasoningPartView(part: ReasoningPart, modifier: Modifier = Modifier) {
    var expanded by remember { mutableStateOf(false) }
    val duration = part.time?.let { t ->
        t.end?.let { end -> "${(end - t.start)}ms" }
    }

    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = modifier
            .padding(horizontal = 14.dp, vertical = 2.dp)
            .clickable { expanded = !expanded },
    ) {
        Text(
            text = buildAnnotatedString {
                withStyle(SpanStyle(color = Secondary, fontFamily = FontFamily.Monospace, fontWeight = FontWeight.Medium)) {
                    append("+ Thought")
                }
                duration?.let {
                    append(" ")
                    withStyle(SpanStyle(color = OnSurfaceFaint, fontFamily = FontFamily.Monospace)) {
                        append(it)
                    }
                }
            },
            fontSize = 13.sp,
        )
        Spacer(Modifier.weight(1f))
        Icon(
            if (expanded) Icons.Default.ExpandLess else Icons.Default.ExpandMore,
            contentDescription = if (expanded) "Collapse" else "Expand",
            tint = OnSurfaceVariant,
            modifier = Modifier.size(16.dp),
        )
    }

    if (expanded) {
        Text(
            text = part.text,
            style = MaterialTheme.typography.bodySmall.copy(
                fontStyle = FontStyle.Italic,
                fontSize = 13.sp,
            ),
            color = OnSurfaceVariant,
            modifier = Modifier.padding(start = 14.dp, end = 14.dp, bottom = 4.dp),
        )
    }
}

// ─── Tool ─────────────────────────────────────────────────────────────────────

@Composable
private fun ToolPartView(part: ToolPart, modifier: Modifier = Modifier) {
    var expanded by remember { mutableStateOf(false) }

    Column(
        modifier = modifier
            .padding(horizontal = 14.dp, vertical = 4.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(SurfaceContainer)
            .border(1.dp, Hairline, RoundedCornerShape(8.dp)),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .fillMaxWidth()
                .clickable { expanded = !expanded }
                .padding(horizontal = 12.dp, vertical = 10.dp),
        ) {
            ToolStatusIndicator(part.state)
            Spacer(Modifier.width(8.dp))
            Text(
                text = part.tool,
                fontFamily = FontFamily.Monospace,
                fontSize = 13.sp,
                color = OnSurface,
                modifier = Modifier.weight(1f),
            )
            ToolStateMeta(part.state)
            Icon(
                if (expanded) Icons.Default.ExpandLess else Icons.Default.ExpandMore,
                contentDescription = null,
                tint = OnSurfaceVariant,
                modifier = Modifier.size(16.dp),
            )
        }

        if (expanded) {
            HorizontalDivider(color = Hairline)
            ToolStateDetail(part.state)
        }
    }
}

@Composable
private fun ToolStatusIndicator(state: ToolState) {
    val (icon, tint) = when (state) {
        is ToolStatePending -> Icons.Default.Schedule to OnSurfaceFaint
        is ToolStateRunning -> Icons.Default.PlayCircle to Secondary
        is ToolStateCompleted -> Icons.Default.CheckCircle to Tertiary
        is ToolStateError -> Icons.Default.Error to Error
    }
    Icon(icon, contentDescription = state.status, tint = tint, modifier = Modifier.size(16.dp))
}

@Composable
private fun ToolStateMeta(state: ToolState) {
    val label = when (state) {
        is ToolStatePending -> "pending"
        is ToolStateRunning -> state.title ?: "running…"
        is ToolStateCompleted -> {
            val ms = state.time?.let { it.end?.minus(it.start) }
            if (ms != null) "${ms}ms" else "done"
        }
        is ToolStateError -> "error"
    }
    Text(
        text = label,
        fontFamily = FontFamily.Monospace,
        fontSize = 11.sp,
        color = OnSurfaceFaint,
        modifier = Modifier.padding(end = 4.dp),
    )
}

@Composable
private fun ToolStateDetail(state: ToolState) {
    val output = when (state) {
        is ToolStateCompleted -> state.output
        is ToolStateError -> state.error
        else -> null
    }
    output?.let {
        Text(
            text = it,
            fontFamily = FontFamily.Monospace,
            fontSize = 12.sp,
            color = OnSurfaceVariant,
            modifier = Modifier
                .fillMaxWidth()
                .horizontalScroll(rememberScrollState())
                .padding(12.dp),
        )
    }
}

// ─── File ─────────────────────────────────────────────────────────────────────

@Composable
private fun FilePartView(part: FilePart, modifier: Modifier = Modifier) {
    AssistChip(
        onClick = {},
        label = {
            Text(
                text = part.filename ?: part.url,
                fontFamily = FontFamily.Monospace,
                fontSize = 12.sp,
                maxLines = 1,
            )
        },
        leadingIcon = { Icon(Icons.Default.AttachFile, contentDescription = null, modifier = Modifier.size(16.dp)) },
        modifier = modifier.padding(horizontal = 14.dp, vertical = 2.dp),
    )
}
