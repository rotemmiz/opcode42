package dev.opcode42.core.sdk

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Pins the PTY rendering contract: the [TerminalEmulator] interprets the control
 * characters and ANSI escape sequences a shell emits, instead of dumping them as
 * literal garbage. These are the cases the old "append raw bytes" path got wrong.
 */
class TerminalEmulatorTest {

    private fun render(vararg chunks: String): List<String> =
        TerminalEmulator().apply { chunks.forEach { feed(it) } }.render()

    @Test
    fun plainTextAccumulatesOnOneLine() {
        assertEquals(listOf("hello world"), render("hello", " world"))
    }

    @Test
    fun lineFeedStartsANewLine() {
        assertEquals(listOf("a", "b", "c"), render("a\nb\nc"))
    }

    @Test
    fun carriageReturnOverwritesInPlace() {
        // Progress-bar idiom: "100%" overwrites the start of "  0%".
        assertEquals(listOf("100%"), render("  0%\r100%"))
    }

    @Test
    fun carriageReturnThenShorterTextKeepsTail() {
        // "abcdef" then \r then "XY" → "XYcdef" (only the overwritten head changes).
        assertEquals(listOf("XYcdef"), render("abcdef\rXY"))
    }

    @Test
    fun backspaceMovesCursorLeft() {
        // type "abc", backspace, type "X" → "abX".
        assertEquals(listOf("abX"), render("abc\bX"))
    }

    @Test
    fun tabAdvancesToNextStop() {
        assertEquals(listOf("a       b"), render("a\tb")) // col 1 → col 8
    }

    @Test
    fun sgrColorSequencesAreStripped() {
        // ESC[31m red ESC[0m → just the visible text, no escape codes.
        val out = render("[31mred[0m text")
        assertEquals(listOf("red text"), out)
    }

    @Test
    fun cursorForwardThenWriteOverwritesWithPadding() {
        // "ab" then CSI 3 C (forward 3) then "X": cursor at col 5 → "ab   X".
        assertEquals(listOf("ab   X"), render("ab[3CX"))
    }

    @Test
    fun cursorBackThenOverwrite() {
        // "abcdef" then CSI 2 D (back 2) then "XY" → "abcdXY".
        assertEquals(listOf("abcdXY"), render("abcdef[2DXY"))
    }

    @Test
    fun absoluteColumnSequence() {
        // "abcdef" then CSI 1 G (column 1) then "Z" → "Zbcdef".
        assertEquals(listOf("Zbcdef"), render("abcdef[1GZ"))
    }

    @Test
    fun eraseInLineToEnd() {
        // "abcdef", \r, CSI 0 K (erase from cursor) → empty line, then "hi".
        assertEquals(listOf("hi"), render("abcdef\r[Khi"))
    }

    @Test
    fun eraseWholeLine() {
        // CSI 2K clears the line but does NOT move the cursor (ANSI), so the
        // following text is written at the cursor's old column (after "garbage").
        assertEquals(listOf("       new"), render("garbage[2Knew"))
    }

    @Test
    fun eraseWholeLineThenCarriageReturn() {
        // The common "clear this line" idiom: CSI 2K then \r resets to col 0.
        assertEquals(listOf("new"), render("garbage[2K\rnew"))
    }

    @Test
    fun oscTitleSequenceIsConsumed() {
        // Set-title OSC (ESC ] 0 ; title BEL) must not appear in the output.
        assertEquals(listOf("prompt$ "), render("]0;my-titleprompt$ "))
    }

    @Test
    fun oscTerminatedByStringTerminator() {
        // OSC terminated by ST (ESC \) instead of BEL.
        assertEquals(listOf("ok"), render("]0;t\\ok"))
    }

    @Test
    fun privateModeSequencesAreNoOps() {
        // CSI ? 25 l (hide cursor) / CSI ? 25 h (show) carry no visible text.
        assertEquals(listOf("xy"), render("[?25lx[?25hy"))
    }

    @Test
    fun escapeSplitAcrossChunksIsHandled() {
        // The CSI sequence is delivered in two feeds — state must survive the boundary.
        assertEquals(listOf("red"), render("[31", "mred"))
    }

    @Test
    fun charsetDesignationEscapeIsConsumed() {
        // ESC ( B (designate ASCII) is a two-byte escape; its selector 'B' must
        // NOT leak into the output. Shells commonly emit this at init.
        assertEquals(listOf("ok"), render("(Bok"))
        assertEquals(listOf("ok"), render(")0ok"))
        assertEquals(listOf("ok"), render("#8ok"))
    }

    @Test
    fun charsetDesignationSplitAcrossChunks() {
        assertEquals(listOf("ok"), render("(", "Bok"))
    }

    @Test
    fun selfContainedEscapeIsConsumed() {
        // ESC c (RIS), ESC 7 (save cursor): single-char escapes, no selector byte.
        assertEquals(listOf("xy"), render("cx7y"))
    }

    @Test
    fun belCharacterIsIgnored() {
        assertEquals(listOf("ab"), render("ab"))
    }

    @Test
    fun maxLinesBoundsBuffer() {
        val emu = TerminalEmulator(maxLines = 3)
        emu.feed("a\nb\nc\nd\ne")
        // Only the last 3 lines are retained.
        assertEquals(listOf("c", "d", "e"), emu.render())
    }

    @Test
    fun clearResetsToSingleEmptyLine() {
        val emu = TerminalEmulator()
        emu.feed("a\nb")
        emu.clear()
        assertEquals(listOf(""), emu.render())
    }
}
