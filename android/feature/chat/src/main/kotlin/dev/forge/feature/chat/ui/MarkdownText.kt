package dev.forge.feature.chat.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// ─── Block types ──────────────────────────────────────────────────────────────

private sealed class MdBlock {
    data class Header(val level: Int, val text: String) : MdBlock()
    data class CodeBlock(val lang: String?, val lines: List<String>) : MdBlock()
    data class Paragraph(val text: String) : MdBlock()
    data class ListItem(val index: Int?, val text: String) : MdBlock() // null index = bullet
    data class Table(val header: List<String>, val rows: List<List<String>>) : MdBlock()
    data object Divider : MdBlock()
}

private val TableSeparator = Regex("""^\|?[\s:|-]*-[\s:|-]*\|?$""")

private fun splitTableRow(line: String): List<String> =
    line.trim().trim('|').split('|').map { it.trim() }

// ─── Parser ───────────────────────────────────────────────────────────────────

private fun parse(markdown: String): List<MdBlock> {
    val lines = markdown.lines()
    val blocks = mutableListOf<MdBlock>()
    var i = 0
    val paraLines = mutableListOf<String>()

    fun flushPara() {
        if (paraLines.isNotEmpty()) {
            blocks += MdBlock.Paragraph(paraLines.joinToString("\n"))
            paraLines.clear()
        }
    }

    while (i < lines.size) {
        val line = lines[i]
        val trimmed = line.trimStart()

        when {
            // Code fence
            trimmed.startsWith("```") -> {
                flushPara()
                val lang = trimmed.removePrefix("```").trim().takeIf { it.isNotEmpty() }
                val codeLines = mutableListOf<String>()
                i++
                while (i < lines.size && !lines[i].trimStart().startsWith("```")) {
                    codeLines += lines[i]
                    i++
                }
                blocks += MdBlock.CodeBlock(lang, codeLines)
            }
            // ATX header
            trimmed.startsWith("# ") -> { flushPara(); blocks += MdBlock.Header(1, trimmed.drop(2)) }
            trimmed.startsWith("## ") -> { flushPara(); blocks += MdBlock.Header(2, trimmed.drop(3)) }
            trimmed.startsWith("### ") -> { flushPara(); blocks += MdBlock.Header(3, trimmed.drop(4)) }
            trimmed.startsWith("#### ") -> { flushPara(); blocks += MdBlock.Header(4, trimmed.drop(5)) }
            // GFM table: a `|`-row immediately followed by a `---` separator row
            trimmed.startsWith("|") && i + 1 < lines.size &&
                TableSeparator.matches(lines[i + 1].trim()) && lines[i + 1].contains('-') -> {
                flushPara()
                val header = splitTableRow(trimmed)
                var j = i + 2
                val rows = mutableListOf<List<String>>()
                while (j < lines.size && lines[j].trimStart().startsWith("|")) {
                    rows += splitTableRow(lines[j].trim())
                    j++
                }
                blocks += MdBlock.Table(header, rows)
                i = j - 1 // outer i++ advances past the last consumed row
            }
            // Divider
            trimmed == "---" || trimmed == "***" || trimmed == "___" -> { flushPara(); blocks += MdBlock.Divider }
            // Ordered list item
            Regex("""^(\d+)\.\s+(.+)""").matches(trimmed) -> {
                val m = Regex("""^(\d+)\.\s+(.+)""").find(trimmed)!!
                flushPara()
                blocks += MdBlock.ListItem(m.groupValues[1].toIntOrNull(), m.groupValues[2])
            }
            // Unordered list item
            (trimmed.startsWith("- ") || trimmed.startsWith("* ") || trimmed.startsWith("+ ")) -> {
                flushPara()
                blocks += MdBlock.ListItem(null, trimmed.drop(2))
            }
            // Blank line — paragraph break
            trimmed.isEmpty() -> flushPara()
            // Normal text
            else -> paraLines += line
        }
        i++
    }
    flushPara()
    return blocks
}

// ─── Inline span parser ───────────────────────────────────────────────────────

internal fun buildInlineSpans(
    text: String,
    codeColor: Color,
    linkColor: Color,
): AnnotatedString = buildAnnotatedString {
    var pos = 0
    while (pos < text.length) {
        when {
            // Inline code: `code`
            text[pos] == '`' -> {
                val end = text.indexOf('`', pos + 1)
                if (end > pos) {
                    withStyle(SpanStyle(fontFamily = ForgeMono, color = codeColor, fontSize = 13.sp)) {
                        append(text.substring(pos + 1, end))
                    }
                    pos = end + 1
                } else { append(text[pos++]) }
            }
            // Bold+italic: ***text***
            text.startsWith("***", pos) -> {
                val end = text.indexOf("***", pos + 3)
                if (end > pos) {
                    withStyle(SpanStyle(fontWeight = FontWeight.Bold, fontStyle = FontStyle.Italic)) {
                        append(text.substring(pos + 3, end))
                    }
                    pos = end + 3
                } else { append(text[pos++]) }
            }
            // Bold: **text**
            text.startsWith("**", pos) -> {
                val end = text.indexOf("**", pos + 2)
                if (end > pos) {
                    withStyle(SpanStyle(fontWeight = FontWeight.Bold)) {
                        append(text.substring(pos + 2, end))
                    }
                    pos = end + 2
                } else { append(text[pos++]) }
            }
            // Italic: *text*
            text[pos] == '*' -> {
                val end = text.indexOf('*', pos + 1)
                if (end > pos) {
                    withStyle(SpanStyle(fontStyle = FontStyle.Italic)) {
                        append(text.substring(pos + 1, end))
                    }
                    pos = end + 1
                } else { append(text[pos++]) }
            }
            // Markdown link: [text](url)
            text[pos] == '[' -> {
                val textEnd = text.indexOf(']', pos + 1)
                if (textEnd > pos && textEnd + 1 < text.length && text[textEnd + 1] == '(') {
                    val urlEnd = text.indexOf(')', textEnd + 2)
                    if (urlEnd > textEnd) {
                        withStyle(SpanStyle(color = linkColor, textDecoration = TextDecoration.Underline)) {
                            append(text.substring(pos + 1, textEnd))
                        }
                        pos = urlEnd + 1
                    } else { append(text[pos++]) }
                } else { append(text[pos++]) }
            }
            // @-mention: @path → cyan mono (design user-turn / prose idiom)
            text[pos] == '@' && (pos == 0 || text[pos - 1].isWhitespace()) -> {
                var end = pos + 1
                while (end < text.length && !text[end].isWhitespace()) end++
                if (end > pos + 1) {
                    withStyle(SpanStyle(color = linkColor, fontFamily = ForgeMono)) {
                        append(text.substring(pos, end))
                    }
                    pos = end
                } else { append(text[pos++]) }
            }
            else -> append(text[pos++])
        }
    }
}

// ─── Composable ───────────────────────────────────────────────────────────────

@Composable
fun MarkdownText(text: String, modifier: Modifier = Modifier) {
    val blocks = parse(text)
    Column(modifier = modifier) {
        for (block in blocks) {
            when (block) {
                is MdBlock.Header -> HeaderBlock(block)
                is MdBlock.CodeBlock -> CodeBlockView(block)
                is MdBlock.Paragraph -> ParagraphBlock(block)
                is MdBlock.ListItem -> ListItemBlock(block)
                is MdBlock.Table -> TableBlock(block)
                is MdBlock.Divider -> androidx.compose.material3.HorizontalDivider(
                    color = Hairline,
                    modifier = Modifier.padding(horizontal = 14.dp, vertical = 4.dp),
                )
            }
        }
    }
}

@Composable
private fun HeaderBlock(block: MdBlock.Header) {
    when (block.level) {
        1 -> Text(
            text = block.text,
            fontSize = 18.sp,
            fontWeight = FontWeight.Bold,
            color = HeaderPurple,
            modifier = Modifier.padding(start = 14.dp, top = 7.dp, end = 14.dp, bottom = 1.dp),
        )
        2 -> Text(
            text = block.text,
            fontSize = 16.sp,
            fontWeight = FontWeight.SemiBold,
            color = HeaderPurple,
            modifier = Modifier.padding(start = 14.dp, top = 6.dp, end = 14.dp, bottom = 1.dp),
        )
        else -> Text(
            text = block.text.uppercase(),
            fontSize = 11.sp,
            fontWeight = FontWeight.Bold,
            fontFamily = ForgeMono,
            color = HeaderPurple,
            letterSpacing = 1.sp,
            modifier = Modifier.padding(start = 14.dp, top = 6.dp, end = 14.dp, bottom = 1.dp),
        )
    }
}

@Composable
private fun CodeBlockView(block: MdBlock.CodeBlock) {
    Box(
        modifier = Modifier
            .padding(horizontal = 14.dp, vertical = 3.dp)
            .fillMaxWidth()
            .background(SurfaceContainerLowest, ForgeShapes.xs)
            .border(1.dp, Hairline, ForgeShapes.xs),
    ) {
        Text(
            text = block.lines.joinToString("\n"),
            fontFamily = ForgeMono,
            fontSize = 12.sp,
            lineHeight = 18.sp,
            color = OnSurface,
            modifier = Modifier
                .horizontalScroll(rememberScrollState())
                .padding(12.dp),
        )
    }
}

@Composable
private fun ParagraphBlock(block: MdBlock.Paragraph) {
    Text(
        text = buildInlineSpans(block.text, codeColor = Secondary, linkColor = LinkCyan),
        fontSize = 14.5.sp,
        lineHeight = 20.sp,
        color = OnSurface,
        modifier = Modifier.padding(horizontal = 14.dp, vertical = 1.dp),
    )
}

@Composable
private fun ListItemBlock(block: MdBlock.ListItem) {
    Row(
        modifier = Modifier.padding(start = 14.dp, end = 14.dp, top = 1.dp, bottom = 1.dp),
    ) {
        Text(
            text = if (block.index != null) "${block.index}." else "•",
            fontSize = 14.sp,
            fontFamily = ForgeMono,
            fontWeight = FontWeight.Bold,
            color = Tertiary,
        )
        Text(
            text = buildInlineSpans(block.text, codeColor = Secondary, linkColor = LinkCyan),
            fontSize = 14.sp,
            lineHeight = 20.sp,
            color = OnSurface,
            modifier = Modifier.weight(1f).padding(start = 10.dp), // mock gap:10 counter→text
        )
    }
}

@Composable
private fun TableBlock(block: MdBlock.Table) {
    val cols = block.header.size.coerceAtLeast(1)
    Column(
        modifier = Modifier
            .padding(horizontal = 14.dp, vertical = 4.dp)
            .clip(ForgeShapes.sm)
            .border(1.dp, Hairline, ForgeShapes.sm),
    ) {
        // Header row — purple, bold mono (design table-header color)
        Row(modifier = Modifier.fillMaxWidth().background(SurfaceContainerHigh)) {
            for (c in 0 until cols) {
                TableCell(block.header.getOrElse(c) { "" }, color = HeaderPurple, weight = FontWeight.Bold)
            }
        }
        block.rows.forEach { row ->
            androidx.compose.material3.HorizontalDivider(color = Hairline)
            Row(modifier = Modifier.fillMaxWidth()) {
                for (c in 0 until cols) {
                    TableCell(row.getOrElse(c) { "" }, color = OnSurface, weight = FontWeight.Normal)
                }
            }
        }
    }
}

@Composable
private fun RowScope.TableCell(text: String, color: Color, weight: FontWeight) {
    Text(
        text = buildInlineSpans(text, codeColor = Secondary, linkColor = LinkCyan),
        fontSize = 13.sp,
        lineHeight = 18.sp,
        color = color,
        fontWeight = weight,
        modifier = Modifier
            .weight(1f)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    )
}
