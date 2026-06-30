package dev.opcode42.core.design.text

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontStyle
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SyntaxHighlightTest {

    private val colors = SyntaxColors(
        keyword = Color(0xFFB08CD4),
        string = Color(0xFF8CC265),
        number = Color(0xFFD99A4E),
        comment = Color(0xFF585F67),
        type = Color(0xFF5FB3C4),
        plain = Color(0xFFD6DADE),
    )

    /** The color applied to the substring starting at [index] (first matching span). */
    private fun colorAt(code: String, index: Int): Color? =
        highlightCode(code, colors).spanStyles
            .firstOrNull { index >= it.start && index < it.end }
            ?.item?.color

    @Test fun keywordsAreColored() {
        val code = "val x = 5"
        assertEquals(colors.keyword, colorAt(code, 0)) // "val"
    }

    @Test fun numbersAreColored() {
        val code = "val x = 42"
        assertEquals(colors.number, colorAt(code, code.indexOf("42")))
    }

    @Test fun stringsAreColored() {
        val code = """val s = "hello""""
        assertEquals(colors.string, colorAt(code, code.indexOf('"')))
    }

    @Test fun lineCommentsAreColoredAndItalic() {
        val code = "x = 1 // a comment"
        val idx = code.indexOf("//")
        assertEquals(colors.comment, colorAt(code, idx))
        val span = highlightCode(code, colors).spanStyles.first { idx >= it.start && idx < it.end }
        assertEquals(FontStyle.Italic, span.item.fontStyle)
    }

    @Test fun blockCommentsAreColored() {
        val code = "a /* note */ b"
        assertEquals(colors.comment, colorAt(code, code.indexOf("/*")))
    }

    @Test fun capitalizedIdentifiersAreTypes() {
        val code = "val list: MutableList = x"
        assertEquals(colors.type, colorAt(code, code.indexOf("MutableList")))
    }

    @Test fun identifierWithTrailingDigitStaysPlain() {
        // `x2` must not be split into an identifier + amber number.
        val code = "x2 = 1"
        assertEquals(colors.plain, colorAt(code, 0))
        assertEquals(colors.plain, colorAt(code, 1)) // the '2' is part of the identifier
    }

    @Test fun preservesFullText() {
        val code = "fun f() { return 7 } // done"
        assertEquals(code, highlightCode(code, colors).text)
    }

    @Test fun emptyAndUnterminatedDoNotThrow() {
        assertEquals("", highlightCode("", colors).text)
        val unterminated = """val s = "oops"""
        assertTrue(highlightCode(unterminated, colors).text == unterminated)
    }

    @Test fun trailingBackslashAtEofDoesNotOverrun() {
        // String ending in a lone backslash at EOF: the \-skip must not read past n.
        val code = "\"a\\" // the three chars: " a \
        assertEquals(code, highlightCode(code, colors).text)
        assertEquals(colors.string, colorAt(code, 0))
    }

    @Test fun escapedQuoteStaysInsideString() {
        val code = """x = "a\"b" + 1"""
        assertEquals(colors.string, colorAt(code, code.indexOf('"'))) // whole "a\"b"
        assertEquals(colors.number, colorAt(code, code.lastIndexOf('1')))
        assertEquals(code, highlightCode(code, colors).text)
    }

    @Test fun unterminatedBlockCommentDoesNotThrow() {
        val code = "a /* x"
        assertEquals(code, highlightCode(code, colors).text)
        assertEquals(colors.comment, colorAt(code, code.indexOf("/*")))
    }

    @Test fun blockCommentIsItalic() {
        val code = "/* c */"
        val span = highlightCode(code, colors).spanStyles.first { 0 >= it.start && 0 < it.end }
        assertEquals(FontStyle.Italic, span.item.fontStyle)
    }

    @Test fun hexAndFloatLiteralsAreNumbers() {
        assertEquals(colors.number, colorAt("x = 0xFF", "x = ".length))
        assertEquals(colors.number, colorAt("y = 3.14f", "y = ".length))
    }
}
