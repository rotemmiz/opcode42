package dev.forge.feature.chat.ui

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
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Size
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
fun PartRenderer(
    part: Part,
    modifier: Modifier = Modifier,
    diffs: Map<String, List<SnapshotFileDiff>> = emptyMap(),
) {
    when (part) {
        is TextPart -> TextPartView(part, modifier)
        is ReasoningPart -> ReasoningPartView(part, modifier)
        is ToolPart -> when {
            part.isHiddenFromRows() -> Unit
            part.tool.lowercase() == "task" -> SubAgentBlock(part, modifier)
            part.rendersAsOwnBlock() -> ToolOutputBlock(part, modifier)
            else -> ToolRowGroup(listOf(part), modifier)
        }
        is FilePart -> FilePartView(part, modifier)
        is PatchPart -> PatchPartView(part, modifier, diffs[part.messageID] ?: emptyList())
        is StepStartPart, is StepFinishPart -> Unit  // invisible separators
        is UnknownPart -> Unit
    }
}

// ─── Text ─────────────────────────────────────────────────────────────────────

@Composable
private fun TextPartView(part: TextPart, modifier: Modifier = Modifier) {
    if (part.text.isBlank()) return
    MarkdownText(text = part.text, modifier = modifier.padding(vertical = 2.dp))
}

// ─── Reasoning ────────────────────────────────────────────────────────────────

@Composable
private fun ReasoningPartView(part: ReasoningPart, modifier: Modifier = Modifier) {
    var expanded by remember { mutableStateOf(false) }
    val duration = part.time?.let { t ->
        t.end?.let { end -> "${(end - t.start)}ms" }
    }

    // Minimal mono line per design (`+ Thought: 740ms`); tap to reveal the
    // full reasoning. No chevron — the line itself is the affordance.
    Text(
        text = buildAnnotatedString {
            withStyle(SpanStyle(color = Secondary, fontFamily = FontFamily.Monospace)) {
                append(if (duration != null) "+ Thought:" else "+ Thought")
            }
            duration?.let {
                append(" ")
                withStyle(SpanStyle(color = OnSurfaceFaint, fontFamily = FontFamily.Monospace)) { append(it) }
            }
        },
        fontSize = 13.sp,
        modifier = modifier
            .clickable { expanded = !expanded }
            .padding(horizontal = 14.dp, vertical = 2.dp),
    )

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

// ─── Patch / Diff ─────────────────────────────────────────────────────────────

@Composable
private fun PatchPartView(
    part: PatchPart,
    modifier: Modifier = Modifier,
    fileDiffs: List<SnapshotFileDiff> = emptyList(),
) {
    var expanded by remember { mutableStateOf(false) }
    val fileCount = part.files.size
    val additions = fileDiffs.sumOf { it.additions }
    val deletions = fileDiffs.sumOf { it.deletions }
    val hasCounts = fileDiffs.isNotEmpty() && (additions > 0 || deletions > 0)

    // Captured for the (non-composable) drawBehind lambda.
    val railColor = Secondary
    val activeTint = Secondary.copy(alpha = 0.10f)

    Column(
        modifier = modifier
            .padding(horizontal = 14.dp, vertical = 4.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(SurfaceContainer)
            .border(1.dp, OutlineVariant, RoundedCornerShape(8.dp)),
    ) {
        // Header — when active (expanded), the TUI amber rail: a 2dp amber
        // inset-start bar over a faint amber tint (design §2).
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 46.dp)
                .then(
                    if (expanded) {
                        Modifier
                            .background(activeTint)
                            .drawBehind {
                                drawRect(railColor, size = Size(2.dp.toPx(), size.height))
                            }
                    } else {
                        Modifier
                    },
                )
                .clickable { expanded = !expanded }
                .padding(horizontal = 14.dp), // centered in 46dp, no vertical pad (mock)
        ) {
            Icon(
                if (expanded) Icons.Default.ExpandLess else Icons.Default.ExpandMore,
                contentDescription = null,
                tint = if (expanded) Secondary else OnSurfaceVariant,
                modifier = Modifier.size(16.dp),
            )
            Spacer(Modifier.width(10.dp))
            Text(
                text = buildAnnotatedString {
                    withStyle(SpanStyle(color = OnSurface)) { append("Edit ") }
                    withStyle(SpanStyle(color = Tertiary)) {
                        append(if (fileCount == 1) part.files.first().substringAfterLast('/') else "$fileCount files")
                    }
                },
                fontFamily = FontFamily.Monospace,
                fontSize = 13.sp,
                maxLines = 1,
                overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            if (hasCounts) {
                Text(
                    text = buildAnnotatedString {
                        withStyle(SpanStyle(color = Tertiary)) { append("+$additions") }
                        append(" ")
                        withStyle(SpanStyle(color = Error)) { append("−$deletions") }
                    },
                    fontFamily = FontFamily.Monospace,
                    fontSize = 12.5.sp,
                )
            } else {
                Text(
                    text = part.hash.take(7),
                    fontFamily = FontFamily.Monospace,
                    fontSize = 11.sp,
                    color = OnSurfaceFaint,
                )
            }
        }

        if (expanded && (fileDiffs.isNotEmpty() || part.files.isNotEmpty())) {
            HorizontalDivider(color = Hairline)
            if (fileDiffs.isNotEmpty()) {
                UnifiedDiffView(diffs = fileDiffs)
            } else {
                // Diff not yet loaded — show file list as placeholder
                part.files.forEach { file ->
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 12.dp, vertical = 6.dp),
                    ) {
                        Icon(
                            Icons.Default.Edit,
                            contentDescription = null,
                            tint = OnSurfaceFaint,
                            modifier = Modifier.size(14.dp),
                        )
                        Spacer(Modifier.width(6.dp))
                        Text(
                            text = file,
                            fontFamily = FontFamily.Monospace,
                            fontSize = 12.sp,
                            color = OnSurfaceVariant,
                            maxLines = 1,
                            overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
                        )
                    }
                }
            }
        }
    }
}

// ─── Unified Diff ─────────────────────────────────────────────────────────────

private val DiffAddBg = Color(0x228CC265)
private val DiffRemoveBg = Color(0x22E0606E)
private val DiffHunkBg = Color(0x1AB08CD4)

@Composable
fun UnifiedDiffView(diffs: List<SnapshotFileDiff>, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .background(SurfaceContainerLowest),
    ) {
        diffs.forEach { diff ->
            // File header — redundant for a single-file card (the card header
            // already names it), so only shown when several files are bundled.
            if (diffs.size > 1) {
                Text(
                    text = diff.file ?: "",
                    fontFamily = FontFamily.Monospace,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.Medium,
                    color = OnSurfaceVariant,
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(SurfaceContainerHigh)
                        .padding(horizontal = 12.dp, vertical = 5.dp),
                )
            }
            // Patch lines — design DiffRow grammar: a 1-char gutter sign column
            // (colored) + body text in onSurface; whole-line tint for add/del,
            // purple hunks, red/cyan ---/+++ headers (design §2).
            val patch = diff.patch
            if (!patch.isNullOrEmpty()) {
                val scrollState = rememberScrollState()
                Box(modifier = Modifier.horizontalScroll(scrollState)) {
                    // IntrinsicSize.Max → all rows share the widest line's width,
                    // so add/del tints span the full row (design DiffRow look).
                    Column(modifier = Modifier.width(IntrinsicSize.Max).padding(vertical = 6.dp)) {
                        patch.lines().forEach { line -> DiffLine(line) }
                    }
                }
            }
        }
    }
}

@Composable
private fun DiffLine(line: String) {
    // (bg, sign, signColor, textColor); empty sign = no gutter (headers/hunks).
    data class Spec(val bg: Color, val sign: String, val signColor: Color, val text: Color, val body: String)
    val s = when {
        line.startsWith("+++") -> Spec(Color.Transparent, "", Color.Transparent, LinkCyan, line)
        line.startsWith("---") -> Spec(Color.Transparent, "", Color.Transparent, Error, line)
        line.startsWith("@@") -> Spec(DiffHunkBg, "", Color.Transparent, HeaderPurple, line)
        line.startsWith("Index:") || line.startsWith("===") ->
            Spec(Color.Transparent, "", Color.Transparent, OnSurfaceFaint, line)
        line.startsWith("+") -> Spec(DiffAddBg, "+", Tertiary, OnSurface, line.drop(1))
        line.startsWith("-") -> Spec(DiffRemoveBg, "−", Error, OnSurface, line.drop(1))
        else -> Spec(Color.Transparent, " ", Color.Transparent, OnSurfaceVariant, line.removePrefix(" "))
    }
    Text(
        text = buildAnnotatedString {
            if (s.sign.isNotEmpty()) withStyle(SpanStyle(color = s.signColor)) { append(s.sign) }
            withStyle(SpanStyle(color = s.text)) { append(s.body.ifEmpty { " " }) }
        },
        fontFamily = FontFamily.Monospace,
        fontSize = 12.sp,
        lineHeight = 20.sp,
        softWrap = false,
        modifier = Modifier
            .fillMaxWidth()
            .background(s.bg)
            .padding(horizontal = 8.dp),
    )
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
