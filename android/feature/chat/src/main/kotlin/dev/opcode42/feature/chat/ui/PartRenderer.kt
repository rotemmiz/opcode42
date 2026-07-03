package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import dev.opcode42.core.model.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
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
    childMessages: Map<String, List<Message>> = emptyMap(),
    onLoadChildSession: (String) -> Unit = {},
    onNavigateToSession: (String) -> Unit = {},
) {
    when (part) {
        is TextPart -> TextPartView(part, modifier)
        is ReasoningPart -> ReasoningPartView(part, modifier)
        is ToolPart -> when {
            part.isHiddenFromRows() -> Unit
            part.tool.lowercase() == "task" -> {
                val childId = remember(part) { part.childSessionId() }
                SubAgentBlock(
                    part = part,
                    modifier = modifier,
                    childMessages = childId?.let { childMessages[it] } ?: emptyList(),
                    onLoadChild = onLoadChildSession,
                    onNavigateToSession = onNavigateToSession,
                )
            }
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

// Whole added/removed lines get a subtle tint; the specific words the intra-line
// word diff flagged as changed get the stronger highlight layered on top.
private val DiffAddBg = Color(0x2E8CC265)
private val DiffRemoveBg = Color(0x2EE0606E)
private val DiffAddWordBg = Color(0x808CC265)
private val DiffRemoveWordBg = Color(0x80E0606E)
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

/** Cap so a pathological pair can't blow up the O(n·m) word-diff LCS; over the
 *  cap we skip word highlighting and the line just shows the subtle line tint. */
private const val MAX_WORD_DIFF_TOKENS = 400
private val WORD = Regex("""\S+""")

/**
 * Per-line char ranges (into the prefix-stripped body) that the intra-line word
 * diff flagged as changed, so only the words that actually differ get the strong
 * highlight. Only paired `-`/`+` replacement lines get ranges; pure adds/removes
 * (e.g. a brand-new file) stay at the subtle line tint.
 */
private fun computeWordHighlights(lines: List<String>): List<List<IntRange>> {
    val out = MutableList(lines.size) { emptyList<IntRange>() }
    fun isRemove(l: String) = l.startsWith("-") && !l.startsWith("---")
    fun isAdd(l: String) = l.startsWith("+") && !l.startsWith("+++")
    var i = 0
    while (i < lines.size) {
        if (!isRemove(lines[i])) { i++; continue }
        val remStart = i
        while (i < lines.size && isRemove(lines[i])) i++
        val remEnd = i
        if (i >= lines.size || !isAdd(lines[i])) continue
        val addStart = i
        while (i < lines.size && isAdd(lines[i])) i++
        val addEnd = i
        // Pair replaced lines by index; ragged remainders keep the subtle tint.
        for (k in 0 until minOf(remEnd - remStart, addEnd - addStart)) {
            val (rem, add) = wordDiff(lines[remStart + k].drop(1), lines[addStart + k].drop(1))
            out[remStart + k] = rem
            out[addStart + k] = add
        }
    }
    return out
}

/** Word-level diff of two line bodies → the changed-word char ranges in each. */
private fun wordDiff(old: String, new: String): Pair<List<IntRange>, List<IntRange>> {
    val oldWords = WORD.findAll(old).toList()
    val newWords = WORD.findAll(new).toList()
    val n = oldWords.size
    val m = newWords.size
    if (n == 0 || m == 0 || n > MAX_WORD_DIFF_TOKENS || m > MAX_WORD_DIFF_TOKENS) {
        return emptyList<IntRange>() to emptyList()
    }
    // LCS over word text; tokens outside the LCS are the changes.
    val dp = Array(n + 1) { IntArray(m + 1) }
    for (a in n - 1 downTo 0) for (b in m - 1 downTo 0) {
        dp[a][b] = if (oldWords[a].value == newWords[b].value) dp[a + 1][b + 1] + 1
        else maxOf(dp[a + 1][b], dp[a][b + 1])
    }
    val oldChanged = BooleanArray(n) { true }
    val newChanged = BooleanArray(m) { true }
    var a = 0
    var b = 0
    while (a < n && b < m) {
        when {
            oldWords[a].value == newWords[b].value -> { oldChanged[a] = false; newChanged[b] = false; a++; b++ }
            dp[a + 1][b] >= dp[a][b + 1] -> a++
            else -> b++
        }
    }
    return mergeChangedWords(oldWords, oldChanged) to mergeChangedWords(newWords, newChanged)
}

/** Merge runs of adjacent changed words into single ranges (bridging the spaces
 *  between them) so a changed phrase reads as one continuous highlight. */
private fun mergeChangedWords(words: List<MatchResult>, changed: BooleanArray): List<IntRange> {
    val ranges = mutableListOf<IntRange>()
    var k = 0
    while (k < words.size) {
        if (!changed[k]) { k++; continue }
        val start = words[k].range.first
        var end = words[k].range.last
        while (k + 1 < words.size && changed[k + 1]) { k++; end = words[k].range.last }
        ranges.add(start..end)
        k++
    }
    return ranges
}

@Composable
fun UnifiedDiffView(diffs: List<SnapshotFileDiff>, modifier: Modifier = Modifier) {
    // Strip git-diff cruft so the body reads like the design's unified diff.
    // Keyed on `diffs` so it doesn't re-run on every recomposition.
    val perFile = remember(diffs) { diffs.map { it.patch?.lines().orEmpty().filterNot(::isDiffNoise) } }
    val totalLines = remember(perFile) { perFile.sumOf { it.size } }
    // Intra-line word highlights, computed once per diff (not per recomposition/scroll).
    val highlightsPerFile = remember(perFile) { perFile.map(::computeWordHighlights) }
    var budget = MAX_DIFF_LINES
    Column(
        modifier = modifier
            .fillMaxWidth()
            .background(SurfaceContainerLowest),
    ) {
        perFile.forEachIndexed { fileIdx, lines ->
            if (budget <= 0 || lines.isEmpty()) return@forEachIndexed
            val shown = lines.take(budget)
            budget -= shown.size
            val fileHighlights = highlightsPerFile[fileIdx]
            // Lines soft-wrap to the viewport width so long edits stay fully
            // visible — no horizontal scroll, nothing clipped off the right edge.
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(vertical = 8.dp),
            ) {
                shown.forEachIndexed { idx, line ->
                    DiffLine(line, fileHighlights.getOrElse(idx) { emptyList() })
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
private fun DiffLine(line: String, highlights: List<IntRange> = emptyList()) {
    val bg: Color
    val sign: String
    val signColor: Color
    val textColor: Color
    val body: String
    val wordBg: Color?
    when {
        line.startsWith("+++") -> { bg = Color.Transparent; sign = ""; signColor = Color.Transparent; textColor = LinkCyan; body = line; wordBg = null }
        line.startsWith("---") -> { bg = Color.Transparent; sign = ""; signColor = Color.Transparent; textColor = Error; body = line; wordBg = null }
        line.startsWith("@@")  -> { bg = DiffHunkBg;        sign = ""; signColor = Color.Transparent; textColor = HeaderPurple; body = line; wordBg = null }
        line.startsWith("Index:") || line.startsWith("===") ->
                                   { bg = Color.Transparent; sign = ""; signColor = Color.Transparent; textColor = OnSurfaceFaint; body = line; wordBg = null }
        line.startsWith("+")   -> { bg = DiffAddBg;    sign = "+"; signColor = Tertiary;       textColor = OnSurface;       body = line.drop(1); wordBg = DiffAddWordBg }
        line.startsWith("-")   -> { bg = DiffRemoveBg; sign = "−"; signColor = Error;           textColor = OnSurface;       body = line.drop(1); wordBg = DiffRemoveWordBg }
        else                   -> { bg = Color.Transparent; sign = " "; signColor = Color.Transparent; textColor = OnSurfaceVariant; body = line.removePrefix(" "); wordBg = null }
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(bg)
            .padding(horizontal = 8.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Text(
            text = sign,
            style = dev.opcode42.core.design.theme.Opcode42Typography.code,
            color = signColor,
            softWrap = false,
            modifier = Modifier.width(8.dp),
        )
        Text(
            // Wrap long lines instead of scrolling; weight caps width so it wraps.
            // Only the words the word diff flagged get the strong background span.
            text = buildAnnotatedString {
                val shownBody = body.ifEmpty { " " }
                append(shownBody)
                if (wordBg != null) {
                    highlights.forEach { r ->
                        val start = r.first.coerceIn(0, shownBody.length)
                        val end = (r.last + 1).coerceIn(start, shownBody.length)
                        if (end > start) addStyle(SpanStyle(background = wordBg), start, end)
                    }
                }
            },
            style = dev.opcode42.core.design.theme.Opcode42Typography.code,
            color = textColor,
            softWrap = true,
            modifier = Modifier.weight(1f),
        )
    }
}

// ─── File ─────────────────────────────────────────────────────────────────────

@Composable
private fun FilePartView(part: FilePart, modifier: Modifier = Modifier) {
    val isImage = part.mime.startsWith("image/", ignoreCase = true) ||
        part.url.startsWith("data:image/", ignoreCase = true)
    if (isImage) {
        ImageFilePart(part, modifier)
    } else {
        AssistChip(
            onClick = {},
            label = {
                Text(
                    text = fileChipLabel(part),
                    fontFamily = Opcode42Mono,
                    fontSize = 12.sp,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            },
            leadingIcon = { Icon(Icons.Default.AttachFile, contentDescription = null, modifier = Modifier.size(16.dp)) },
            modifier = modifier.padding(horizontal = 14.dp, vertical = 2.dp),
        )
    }
}

/**
 * Image file part: renders a 120dp thumbnail (decoded from a `data:image/...;base64,...` URI
 * or loaded from an HTTP URL) and opens a full-screen zoomable viewer on tap. Non-image files
 * fall back to the chip above. No third-party image loader — base64 decodes inline, HTTP URLs
 * load asynchronously via [produceState].
 */
@Composable
private fun ImageFilePart(part: FilePart, modifier: Modifier = Modifier) {
    var showViewer by remember { mutableStateOf(false) }
    val bitmap by produceState<android.graphics.Bitmap?>(initialValue = null, part.url) {
        value = loadBitmap(part.url)
    }
    val shape = MaterialTheme.shapes.medium
    Box(
        modifier = modifier
            .padding(horizontal = 14.dp, vertical = 4.dp)
            .size(width = 120.dp, height = 120.dp)
            .clip(shape)
            .background(SurfaceContainerLow)
            .clickable { showViewer = true },
        contentAlignment = Alignment.Center,
    ) {
        val bmp = bitmap
        if (bmp != null) {
            androidx.compose.foundation.Image(
                bitmap = bmp.asImageBitmap(),
                contentDescription = part.filename ?: "image",
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
            )
        } else {
            Icon(Icons.Default.AttachFile, contentDescription = null, tint = OnSurfaceFaint)
        }
    }
    if (showViewer) {
        ImageViewerDialog(bitmap = bitmap, name = part.filename ?: "image", onDismiss = { showViewer = false })
    }
}

/** Full-screen zoomable image viewer. Pinch-to-zoom + pan via detectTransformGestures. */
@Composable
private fun ImageViewerDialog(bitmap: android.graphics.Bitmap?, name: String, onDismiss: () -> Unit) {
    var scale by remember { mutableFloatStateOf(1f) }
    var offsetX by remember { mutableFloatStateOf(0f) }
    var offsetY by remember { mutableFloatStateOf(0f) }
    Dialog(onDismissRequest = onDismiss) {
        Box(
            Modifier
                .fillMaxSize()
                .background(Color.Black.copy(alpha = 0.9f))
                .clickable(
                    indication = null,
                    interactionSource = remember { androidx.compose.foundation.interaction.MutableInteractionSource() },
                ) { onDismiss() }
                .pointerInput(Unit) {
                    detectTransformGestures { _, pan, zoom, _ ->
                        scale = (scale * zoom).coerceIn(1f, 5f)
                        offsetX += pan.x
                        offsetY += pan.y
                    }
                },
            contentAlignment = Alignment.Center,
        ) {
            if (bitmap != null) {
                androidx.compose.foundation.Image(
                    bitmap = bitmap.asImageBitmap(),
                    contentDescription = name,
                    contentScale = ContentScale.Fit,
                    modifier = Modifier
                        .fillMaxSize()
                        .graphicsLayer(
                            scaleX = scale,
                            scaleY = scale,
                            translationX = offsetX,
                            translationY = offsetY,
                        ),
                )
            } else {
                Text("Loading…", color = Color.White)
            }
        }
    }
}

/** Decodes a Bitmap from a `data:image/...;base64,...` URI (inline) or an HTTP URL (async). */
private suspend fun loadBitmap(url: String): android.graphics.Bitmap? {
    return try {
        if (url.startsWith("data:", ignoreCase = true)) {
            // data:image/png;base64,<...>
            val commaIdx = url.indexOf(',')
            if (commaIdx < 0) return null
            val b64 = url.substring(commaIdx + 1)
            val bytes = android.util.Base64.decode(b64, android.util.Base64.DEFAULT)
            withContext(Dispatchers.Default) {
                android.graphics.BitmapFactory.decodeByteArray(bytes, 0, bytes.size)
            }
        } else {
            // HTTP(S) URL — fetch bytes off the main thread.
            withContext(Dispatchers.IO) {
                val conn = java.net.URL(url).openConnection() as java.net.HttpURLConnection
                conn.connectTimeout = 10_000
                conn.readTimeout = 10_000
                conn.inputStream.use { input ->
                    val bytes = input.readBytes()
                    android.graphics.BitmapFactory.decodeByteArray(bytes, 0, bytes.size)
                }
            }
        }
    } catch (e: Exception) {
        null
    }
}

/**
 * Short, safe chip label for a file part. NEVER returns a raw `data:` URL — a base64
 * data URL can be many megabytes, and handing that to a Text node blows up text layout
 * (the bug that OOM-killed the app). Prefer the filename; for a data URL fall back to the
 * mime type (or "attachment"); for a real URL use the last path segment. Every branch
 * is capped with `.take(120)` so nothing can ever reach layout at megabyte scale.
 */
internal fun fileChipLabel(part: FilePart): String {
    part.filename?.takeIf { it.isNotBlank() }?.let { return it.take(120) }
    val url = part.url
    return when {
        url.startsWith("data:", ignoreCase = true) -> part.mime.takeIf { it.isNotBlank() }?.take(120) ?: "attachment"
        else -> url.substringAfterLast('/').takeIf { it.isNotBlank() }?.take(120) ?: "attachment"
    }
}
