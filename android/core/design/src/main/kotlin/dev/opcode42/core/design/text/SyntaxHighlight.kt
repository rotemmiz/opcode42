package dev.opcode42.core.design.text

import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.withStyle
import dev.opcode42.core.design.theme.HeaderPurple
import dev.opcode42.core.design.theme.LinkCyan
import dev.opcode42.core.design.theme.OnSurface
import dev.opcode42.core.design.theme.OnSurfaceFaint
import dev.opcode42.core.design.theme.Secondary
import dev.opcode42.core.design.theme.Tertiary

/**
 * Colors for the semantic syntax-token classes, mapped to the design's `sem` table
 * (tokens.css): keyword=purple, string=green, number=amber, comment=faint(italic),
 * type=cyan, plain=on-surface.
 */
data class SyntaxColors(
    val keyword: Color,
    val string: Color,
    val number: Color,
    val comment: Color,
    val type: Color,
    val plain: Color,
) {
    companion object {
        /** The theme-token mapping, resolved against the active Opcode42 scheme. */
        @Composable
        fun fromTheme(): SyntaxColors = SyntaxColors(
            keyword = HeaderPurple,
            string = Tertiary,
            number = Secondary,
            comment = OnSurfaceFaint,
            type = LinkCyan,
            plain = OnSurface,
        )
    }
}

// Cross-language keyword set (Kotlin/JS·TS/Go/Python/Rust/Java/Swift overlap). A token
// is colored as a keyword only on an exact, case-sensitive match.
private val KEYWORDS = setOf(
    "fun", "val", "var", "const", "let", "function", "def", "func", "fn",
    "return", "if", "else", "elif", "for", "while", "do", "when", "match", "switch", "case",
    "break", "continue", "import", "package", "from", "use", "using", "include", "require",
    "class", "interface", "object", "struct", "enum", "trait", "type", "typealias", "data",
    "sealed", "abstract", "open", "override", "public", "private", "protected", "internal",
    "static", "final", "suspend", "async", "await", "yield", "new", "null", "nil", "none",
    "true", "false", "this", "self", "super", "try", "catch", "finally", "throw", "throws",
    "defer", "go", "in", "is", "as", "typeof", "instanceof", "and", "or", "not", "pub", "mut",
    "impl", "where", "extends", "implements", "export", "default",
)

/**
 * Language-agnostic, heuristic syntax highlighting. It colors C-style line/block
 * comments, string literals (`"`, `'`, `` ` ``), numbers, a common cross-language
 * keyword set, and Capitalized type-like identifiers; everything else stays plain.
 * This is deliberately a lightweight approximation of the design's per-token coloring,
 * not a per-language parser — it never throws and degrades to plain text on anything
 * it doesn't recognize.
 */
fun highlightCode(code: String, colors: SyntaxColors): AnnotatedString = buildAnnotatedString {
    val n = code.length
    var i = 0
    val word = StringBuilder()

    fun flushWord() {
        if (word.isEmpty()) return
        val w = word.toString()
        val style = when {
            w in KEYWORDS -> SpanStyle(color = colors.keyword)
            w[0].isUpperCase() -> SpanStyle(color = colors.type)
            else -> SpanStyle(color = colors.plain)
        }
        withStyle(style) { append(w) }
        word.clear()
    }

    while (i < n) {
        val c = code[i]
        when {
            // Line comment: // … (to end of line)
            c == '/' && i + 1 < n && code[i + 1] == '/' -> {
                flushWord()
                val end = code.indexOf('\n', i).let { if (it == -1) n else it }
                withStyle(SpanStyle(color = colors.comment, fontStyle = FontStyle.Italic)) {
                    append(code.substring(i, end))
                }
                i = end
            }
            // Block comment: /* … */
            c == '/' && i + 1 < n && code[i + 1] == '*' -> {
                flushWord()
                val end = code.indexOf("*/", i + 2).let { if (it == -1) n else it + 2 }
                withStyle(SpanStyle(color = colors.comment, fontStyle = FontStyle.Italic)) {
                    append(code.substring(i, end))
                }
                i = end
            }
            // String literal: " … "  ' … '  ` … `  (with \ escapes)
            c == '"' || c == '\'' || c == '`' -> {
                flushWord()
                var j = i + 1
                while (j < n && code[j] != c) {
                    if (code[j] == '\\' && j + 1 < n) j++
                    j++
                }
                val end = (j + 1).coerceAtMost(n)
                withStyle(SpanStyle(color = colors.string)) { append(code.substring(i, end)) }
                i = end
            }
            // Number (only when not continuing an identifier, so `x2` stays an identifier)
            c.isDigit() && word.isEmpty() -> {
                var j = i
                while (j < n && (code[j].isLetterOrDigit() || code[j] == '.' || code[j] == '_')) j++
                withStyle(SpanStyle(color = colors.number)) { append(code.substring(i, j)) }
                i = j
            }
            // Identifier / keyword characters accumulate into `word`
            c.isLetterOrDigit() || c == '_' || c == '$' -> {
                word.append(c)
                i++
            }
            // Anything else (punctuation, whitespace) flushes the word and passes through
            else -> {
                flushWord()
                append(c)
                i++
            }
        }
    }
    flushWord()
}
