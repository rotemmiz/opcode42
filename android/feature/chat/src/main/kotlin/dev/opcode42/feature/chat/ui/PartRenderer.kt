package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.ScrollState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.ui.layout.onSizeChanged
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
import dev.opcode42.core.model.*
import kotlinx.serialization.json.jsonPrimitive

/**
 * Renders a single Part.
 * Design follows design/android/README.md — Terminal-Material direction.
 */
@Composable
fun PartRenderer(
    part: Part,
    modifier: Modifier = Modifier,
    diffs: Map<String, List<SnapshotFileDiff>> = emptyMap(),
    editParts: List<ToolPart> = emptyList(),
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
        is PatchPart -> PatchPartView(part, modifier, diffs[part.messageID] ?: emptyList(), editParts)
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
        t.end?.let { end ->
            // Reasoning time as seconds with 2-decimal precision (e.g. 740ms → "0.74s").
            String.format(java.util.Locale.US, "%.2fs", (end - t.start) / 1000.0)
        }
    }

    // Sans thought line with the amber spark glyph (mobile.md §1's "spark glyph +
    // Thought for …", rendered here in seconds, e.g. "Thought for 0.74s"); tap to
    // reveal the full reasoning. No chevron — the line itself is the affordance.
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = modifier
            .clickable { expanded = !expanded }
            .padding(horizontal = 14.dp, vertical = 3.dp),
    ) {
        Icon(
            Icons.Default.AutoAwesome,
            contentDescription = null,
            tint = Secondary,
            modifier = Modifier.size(13.dp),
        )
        Spacer(Modifier.width(6.dp))
        Text(
            text = buildAnnotatedString {
                withStyle(SpanStyle(color = Secondary)) {
                    append(if (duration != null) "Thought for" else "Thinking…")
                }
                duration?.let {
                    append(" ")
                    withStyle(SpanStyle(color = OnSurfaceFaint)) { append(it) }
                }
            },
            fontSize = 13.sp,
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

// ─── Patch / Diff ─────────────────────────────────────────────────────────────

/** Build a synthetic SnapshotFileDiff from an edit ToolPart's old/new strings. */
internal fun syntheticDiff(editPart: ToolPart, filePath: String): SnapshotFileDiff? {
    val input = editPart.state.inputObject() ?: return null
    // opencode sends camelCase: oldString / newString (snake_case as fallback)
    fun strFor(vararg keys: String) = keys.firstNotNullOfOrNull { key ->
        try { input[key]?.jsonPrimitive?.content } catch (_: Exception) { null }
    }
    val oldStr = strFor("oldString", "old_string") ?: ""
    val newStr = strFor("newString", "new_string") ?: return null
    val shortPath = filePath.substringAfterLast('/')
    val oldLines = oldStr.lines()
    val newLines = newStr.lines()
    val patch = buildString {
        appendLine("--- a/$shortPath")
        appendLine("+++ b/$shortPath")
        appendLine("@@ -1,${oldLines.size} +1,${newLines.size} @@")
        oldLines.forEach { appendLine("-$it") }
        newLines.forEach { appendLine("+$it") }
    }
    return SnapshotFileDiff(
        file = filePath,
        patch = patch,
        additions = newLines.size,
        deletions = oldLines.size,
    )
}

@Composable
private fun PatchPartView(
    part: PatchPart,
    modifier: Modifier = Modifier,
    fileDiffs: List<SnapshotFileDiff> = emptyList(),
    editParts: List<ToolPart> = emptyList(),
) {
    // When the diff API has no data, synthesize diffs from the edit ToolPart inputs.
    // Include `part` as a key so that streaming additions to `part.files` re-run the block.
    val effectiveDiffs = remember(part, fileDiffs, editParts) {
        if (fileDiffs.isNotEmpty()) fileDiffs
        else part.files.mapNotNull { filePath ->
            val ep = editParts.firstOrNull { tp ->
                tp.inputString("file_path", "filePath", "path") == filePath
            } ?: return@mapNotNull null
            syntheticDiff(ep, filePath)
        }
    }
    var expanded by remember { mutableStateOf(false) }
    val fileCount = part.files.size
    val additions = effectiveDiffs.sumOf { it.additions }
    val deletions = effectiveDiffs.sumOf { it.deletions }
    val hasCounts = effectiveDiffs.isNotEmpty() && (additions > 0 || deletions > 0)

    // Captured for the (non-composable) drawBehind lambda.
    val railColor = Secondary
    val activeTint = Secondary.copy(alpha = 0.10f)

    Column(
        modifier = modifier
            .padding(horizontal = 14.dp, vertical = 4.dp)
            .clip(Opcode42Shapes.sm)
            .background(SurfaceContainer)
            .border(1.dp, OutlineVariant, Opcode42Shapes.sm),
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
                fontFamily = Opcode42Mono,
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
                    fontFamily = Opcode42Mono,
                    fontSize = 12.5.sp,
                )
            } else {
                Text(
                    text = part.hash.take(7),
                    fontFamily = Opcode42Mono,
                    fontSize = 11.sp,
                    color = OnSurfaceFaint,
                )
            }
        }

        if (expanded && (effectiveDiffs.isNotEmpty() || part.files.isNotEmpty())) {
            HorizontalDivider(color = Hairline)
            if (effectiveDiffs.isNotEmpty()) {
                UnifiedDiffView(diffs = effectiveDiffs)
            } else {
                // No diff data at all — show file paths as a last-resort placeholder
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
                            fontFamily = Opcode42Mono,
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

/** Cap on rendered diff lines — these render eagerly (no nested LazyColumn is
 *  possible inside the scrolling stream), so a huge snapshot would ANR. */
private const val MAX_DIFF_LINES = 400

/** Git/patch metadata lines the design's clean unified diff omits (it starts at
 *  `--- file`). We keep only `---`/`+++`/`@@`/content rows. */
private fun isDiffNoise(line: String): Boolean =
    line.startsWith("diff --git") || line.startsWith("index ") ||
        line.startsWith("Index:") || line.startsWith("===") ||
        line.startsWith("new file mode") || line.startsWith("deleted file mode") ||
        line.startsWith("old mode") || line.startsWith("new mode") ||
        line.startsWith("similarity index") || line.startsWith("rename ") ||
        line.startsWith("copy ") || line.startsWith("\\ No newline")

@Composable
fun UnifiedDiffView(diffs: List<SnapshotFileDiff>, modifier: Modifier = Modifier) {
    // Strip git-diff cruft so the body reads like the design's unified diff.
    // Keyed on `diffs` so it doesn't re-run on every recomposition.
    val perFile = remember(diffs) { diffs.map { it.patch?.lines().orEmpty().filterNot(::isDiffNoise) } }
    val totalLines = remember(perFile) { perFile.sumOf { it.size } }
    var budget = MAX_DIFF_LINES
    // Track viewport width via onSizeChanged (a layout side-effect) instead of
    // BoxWithConstraints (SubcomposeLayout). SubcomposeLayout inside LazyColumn
    // items triggers a parent re-layout pass when items scroll into view, which
    // was causing the TodoSheet to flicker as it remeasured.
    var viewportWidthPx by remember { mutableIntStateOf(0) }
    val density = androidx.compose.ui.platform.LocalDensity.current
    Column(
        modifier = modifier
            .fillMaxWidth()
            .background(SurfaceContainerLowest)
            .onSizeChanged { viewportWidthPx = it.width },
    ) {
        val viewportWidthDp = with(density) { viewportWidthPx.toDp() }
        diffs.zip(perFile).forEach { (diff, lines) ->
            if (budget <= 0 || lines.isEmpty()) return@forEach
            val shown = lines.take(budget)
            budget -= shown.size
            // Keyed by file path so scroll position is stable when files reorder mid-stream.
            val scrollState = remember(diff.file) { ScrollState(0) }
            Box(modifier = Modifier.horizontalScroll(scrollState)) {
                Column(
                    modifier = Modifier
                        .widthIn(min = viewportWidthDp)
                        .width(IntrinsicSize.Max)
                        .padding(vertical = 8.dp),
                ) {
                    shown.forEach { line -> DiffLine(line) }
                }
            }
        }
        if (totalLines > MAX_DIFF_LINES) {
            Text(
                text = "… ${totalLines - MAX_DIFF_LINES} more lines",
                fontFamily = Opcode42Mono,
                fontSize = 12.sp,
                color = OnSurfaceFaint,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 6.dp),
            )
        }
    }
}

@Composable
private fun DiffLine(line: String) {
    val bg: Color
    val sign: String
    val signColor: Color
    val textColor: Color
    val body: String
    when {
        line.startsWith("+++") -> { bg = Color.Transparent; sign = ""; signColor = Color.Transparent; textColor = LinkCyan; body = line }
        line.startsWith("---") -> { bg = Color.Transparent; sign = ""; signColor = Color.Transparent; textColor = Error; body = line }
        line.startsWith("@@")  -> { bg = DiffHunkBg;        sign = ""; signColor = Color.Transparent; textColor = HeaderPurple; body = line }
        line.startsWith("Index:") || line.startsWith("===") ->
                                   { bg = Color.Transparent; sign = ""; signColor = Color.Transparent; textColor = OnSurfaceFaint; body = line }
        line.startsWith("+")   -> { bg = DiffAddBg;    sign = "+"; signColor = Tertiary;       textColor = OnSurface;       body = line.drop(1) }
        line.startsWith("-")   -> { bg = DiffRemoveBg; sign = "−"; signColor = Error;           textColor = OnSurface;       body = line.drop(1) }
        else                   -> { bg = Color.Transparent; sign = " "; signColor = Color.Transparent; textColor = OnSurfaceVariant; body = line.removePrefix(" ") }
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(bg)
            .padding(horizontal = 8.dp),
    ) {
        Text(
            text = sign,
            fontFamily = Opcode42Mono,
            fontSize = 12.sp,
            lineHeight = 20.sp,
            color = signColor,
            softWrap = false,
            modifier = Modifier.width(8.dp),
        )
        Text(
            text = body.ifEmpty { " " },
            fontFamily = Opcode42Mono,
            fontSize = 12.sp,
            lineHeight = 20.sp,
            color = textColor,
            softWrap = false,
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
                fontFamily = Opcode42Mono,
                fontSize = 12.sp,
                maxLines = 1,
            )
        },
        leadingIcon = { Icon(Icons.Default.AttachFile, contentDescription = null, modifier = Modifier.size(16.dp)) },
        modifier = modifier.padding(horizontal = 14.dp, vertical = 2.dp),
    )
}
