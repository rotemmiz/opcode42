package dev.forge.feature.chat.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.forge.core.model.Part
import dev.forge.core.model.ToolPart
import dev.forge.core.model.ToolState
import dev.forge.core.model.ToolStateCompleted
import dev.forge.core.model.ToolStateError
import dev.forge.core.model.ToolStatePending
import dev.forge.core.model.ToolStateRunning
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive

/**
 * A run of consecutive tool calls collapsed into one hairline card (design §2,
 * "Tool-row group"). Each row uses the TUI glyph grammar:
 *   → Read src/http.ts   ·   * Grep "fetch("  · 2
 */
@Composable
fun ToolRowGroup(parts: List<ToolPart>, modifier: Modifier = Modifier) {
    val rows = parts.map { toolRowOf(it) }
    if (rows.isEmpty()) return
    androidx.compose.foundation.layout.Column(
        modifier = modifier
            .padding(horizontal = 14.dp, vertical = 4.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(SurfaceContainer)
            .border(1.dp, Hairline, RoundedCornerShape(8.dp)),
    ) {
        rows.forEachIndexed { index, row ->
            if (index > 0) HorizontalDivider(color = Hairline)
            ToolRowView(row)
        }
    }
}

private data class ToolRow(
    val glyph: String,
    val label: String,
    val path: String?,
    val meta: String?,
    val metaIsError: Boolean,
)

@Composable
private fun ToolRowView(row: ToolRow) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 44.dp)
            .padding(horizontal = 14.dp, vertical = 8.dp),
    ) {
        Text(
            text = row.glyph,
            fontFamily = FontFamily.Monospace,
            fontSize = 13.sp,
            color = OnSurfaceFaint,
            modifier = Modifier.width(14.dp),
        )
        Text(
            text = row.label,
            fontFamily = FontFamily.Monospace,
            fontSize = 13.sp,
            color = OnSurface,
        )
        if (row.path != null) {
            Text(
                text = row.path,
                fontFamily = FontFamily.Monospace,
                fontSize = 13.sp,
                color = OnSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f, fill = false),
            )
        }
        if (row.meta != null) {
            Text(
                text = row.meta,
                fontFamily = FontFamily.Monospace,
                fontSize = 13.sp,
                color = if (row.metaIsError) Error else OnSurfaceFaint,
                maxLines = 1,
                modifier = Modifier
                    .weight(1f)
                    .wrapContentWidth(Alignment.End),
            )
        }
    }
}

/** Tools that are surfaced elsewhere (todos sheet) and skipped from tool rows. */
internal fun ToolPart.isHiddenFromRows(): Boolean =
    tool == "todowrite" || tool == "todoread"

/** A renderable unit in the stream: a run of tool calls, or any other single part. */
sealed interface RenderItem {
    data class Tools(val parts: List<ToolPart>) : RenderItem
    data class Single(val part: Part) : RenderItem
}

/**
 * Collapses a message's parts into render items: consecutive [ToolPart]s become
 * one [RenderItem.Tools] (rendered as a single grouped card); everything else
 * stays a [RenderItem.Single]. Todo tool calls are dropped (shown in the sheet).
 */
fun groupRenderItems(parts: List<Part>): List<RenderItem> {
    val items = mutableListOf<RenderItem>()
    var pending = mutableListOf<ToolPart>()
    fun flush() {
        if (pending.isNotEmpty()) {
            items += RenderItem.Tools(pending)
            pending = mutableListOf()
        }
    }
    for (part in parts) {
        if (part is ToolPart) {
            if (part.isHiddenFromRows()) continue
            pending += part
        } else {
            flush()
            items += RenderItem.Single(part)
        }
    }
    flush()
    return items
}

private fun toolRowOf(part: ToolPart): ToolRow {
    val tool = part.tool.lowercase()
    val input = part.state.input()
    val glyph = if (tool in setOf("grep", "glob", "list", "ls")) "*" else "→"
    val label = when (tool) {
        "read" -> "Read"
        "write" -> "Write"
        "edit", "patch" -> "Edit"
        "bash" -> "Bash"
        "grep" -> "Grep"
        "glob" -> "Glob"
        "list", "ls" -> "List"
        "webfetch", "fetch" -> "Fetch"
        "task" -> "Task"
        else -> part.tool.replaceFirstChar { it.uppercase() }
    }
    val rawPath = input.firstString("filePath", "path", "pattern", "command", "query", "url", "description")
    val path = when (tool) {
        "grep", "glob" -> rawPath?.let { "\"$it\"" }
        "read", "write", "edit", "patch", "list", "ls" -> rawPath?.substringAfterLast('/')
        else -> rawPath
    }
    val (meta, isError) = when (val s = part.state) {
        is ToolStateRunning -> (s.title ?: "running…") to false
        is ToolStateCompleted -> s.title to false
        is ToolStateError -> "error" to true
        is ToolStatePending -> null to false
    }
    return ToolRow(glyph, label, path, meta, isError)
}

private fun ToolState.input(): JsonObject? = when (this) {
    is ToolStatePending -> input
    is ToolStateRunning -> input
    is ToolStateCompleted -> input
    is ToolStateError -> input
}

private fun JsonObject?.firstString(vararg keys: String): String? {
    if (this == null) return null
    for (key in keys) {
        val v = this[key]?.jsonPrimitive?.contentOrNull()
        if (!v.isNullOrBlank()) return v
    }
    return null
}

private fun kotlinx.serialization.json.JsonPrimitive.contentOrNull(): String? =
    if (isString) content else content.takeIf { it != "null" }
