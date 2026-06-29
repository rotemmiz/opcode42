package dev.opcode42.core.sdk

/**
 * A minimal, pure-Kotlin terminal emulator that turns a raw PTY byte/char stream
 * into a list of rendered text lines.
 *
 * The Opcode42 daemon streams PTY output as **text frames** of UTF-8 terminal output
 * that still contains the raw control characters and ANSI escape sequences the
 * shell emits (`pty/index.ts`: data chunks are raw strings). A naive "append the
 * bytes" renderer shows escape codes as garbage (`^[[0m`, `^[[31m`, …) and gets
 * cursor motion (`\r`, backspace, in-line cursor moves) wrong.
 *
 * This emulator keeps a line buffer with a cursor column and interprets the
 * subset of control sequences a coding-assistant shell actually produces:
 *
 *  - `\n` (LF)        → new line
 *  - `\r` (CR)        → cursor to column 0 (overwrite-in-place, common in progress bars)
 *  - `\b` (BS, 0x08)  → cursor left one column
 *  - `\t` (TAB)       → advance to the next 8-column tab stop
 *  - BEL (0x07)       → ignored
 *  - CSI sequences (`ESC [ … final`):
 *      - `K` (EL)     → erase in line (clears from / to / whole line per param)
 *      - `C` / `D`    → cursor right / left within the line
 *      - `G`          → cursor to absolute column (1-based)
 *      - `m` (SGR)    → colour/style — dropped (we render plain text)
 *      - others       → consumed and dropped (no-ops for a plain text view)
 *  - other ESC sequences (OSC `ESC ] … BEL/ST`, charset `ESC ( `, etc.) → consumed
 *
 * It is deliberately a *line* model (no scroll region / absolute row addressing):
 * absolute cursor-position sequences that move between rows are dropped, which is
 * the pragmatic choice for an append-only chat-style terminal pane. This keeps the
 * class pure-JVM (no Android dependency) so it is exhaustively unit-testable.
 *
 * Not thread-safe; drive it from a single coroutine.
 */
class TerminalEmulator(private val maxLines: Int = 5_000) {

    private val lines = ArrayList<StringBuilder>().apply { add(StringBuilder()) }
    private var cursorCol = 0

    private companion object {
        const val ESC = ''
        const val BEL = ''
        const val BS = '\b'
    }

    /** Snapshot of the current screen as immutable strings. */
    fun render(): List<String> = lines.map { it.toString() }

    /** Reset to a single empty line. */
    fun clear() {
        lines.clear()
        lines.add(StringBuilder())
        cursorCol = 0
        state = State.TEXT
        csiBuf.setLength(0)
    }

    private enum class State { TEXT, ESC, ESC_INTERMEDIATE, CSI, OSC }

    private var state = State.TEXT
    private val csiBuf = StringBuilder()

    /** Feed a decoded chunk of terminal output. May be called with partial sequences. */
    fun feed(text: String) {
        var i = 0
        while (i < text.length) {
            val c = text[i]
            when (state) {
                State.TEXT -> handleText(c)
                State.ESC -> handleEsc(c)
                State.ESC_INTERMEDIATE -> state = State.TEXT // swallow the final selector byte
                State.CSI -> handleCsi(c)
                State.OSC -> handleOsc(c)
            }
            i++
        }
    }

    private fun handleText(c: Char) {
        when (c) {
            ESC -> state = State.ESC
            '\n' -> newLine()
            '\r' -> cursorCol = 0
            BS -> if (cursorCol > 0) cursorCol--
            '\t' -> {
                val next = ((cursorCol / 8) + 1) * 8
                while (cursorCol < next) putChar(' ')
            }
            BEL -> { /* ignore */ }
            else -> if (c >= ' ') putChar(c) // drop other C0 control chars
        }
    }

    private fun handleEsc(c: Char) {
        when (c) {
            '[' -> { csiBuf.setLength(0); state = State.CSI }
            ']' -> state = State.OSC // OSC: ESC ] … (terminated by BEL or ST)
            // Charset-designation / intermediate escapes (ESC ( B, ESC ) 0, ESC # 8,
            // ESC * …, ESC % …) take ONE more selector byte — swallow it so it does
            // not leak into the rendered text.
            '(', ')', '*', '+', '-', '.', '/', '#', '%', ' ' -> state = State.ESC_INTERMEDIATE
            // All other single-char escapes (ESC c, ESC 7/8, ESC M, ESC =, …) are
            // self-contained: drop straight back to TEXT.
            else -> state = State.TEXT
        }
    }

    private fun handleCsi(c: Char) {
        // CSI params/intermediates are 0x20–0x3F; the final byte is 0x40–0x7E.
        if (c in ' '..'?') {
            csiBuf.append(c)
            return
        }
        applyCsi(finalByte = c, params = csiBuf.toString())
        csiBuf.setLength(0)
        state = State.TEXT
    }

    private fun handleOsc(c: Char) {
        // OSC string terminates on BEL (0x07) or ST (ESC \). We swallow until BEL;
        // an ESC inside an OSC starts ST handling — drop back to ESC state so the
        // trailing '\\' is consumed.
        when (c) {
            BEL -> state = State.TEXT
            ESC -> state = State.ESC
        }
    }

    private fun applyCsi(finalByte: Char, params: String) {
        // Ignore private-mode sequences (CSI ? …), which are device toggles.
        if (params.startsWith("?")) return
        val nums = params.split(';').map { it.toIntOrNull() }
        fun arg(idx: Int, default: Int) = nums.getOrNull(idx) ?: default
        when (finalByte) {
            'C' -> cursorCol += arg(0, 1).coerceAtLeast(1)          // cursor forward
            'D' -> cursorCol = (cursorCol - arg(0, 1).coerceAtLeast(1)).coerceAtLeast(0) // back
            'G' -> cursorCol = (arg(0, 1).coerceAtLeast(1) - 1)     // absolute column (1-based)
            'K' -> eraseInLine(arg(0, 0))                            // erase line
            // 'm' (SGR), 'H'/'f' (cursor position — cross-row, dropped), 'J' (erase
            // display), 'A'/'B' (cursor up/down — cross-row), etc. are all no-ops for
            // this single-line append model.
            else -> { /* drop */ }
        }
    }

    /** EL: 0 = cursor→end, 1 = start→cursor, 2 = whole line. */
    private fun eraseInLine(mode: Int) {
        val line = lines.last()
        when (mode) {
            0 -> if (cursorCol < line.length) line.setLength(cursorCol)
            1 -> {
                val end = minOf(cursorCol + 1, line.length)
                for (j in 0 until end) line.setCharAt(j, ' ')
            }
            2 -> line.setLength(0)
        }
    }

    private fun putChar(c: Char) {
        val line = lines.last()
        // Pad with spaces if the cursor jumped past the current end (e.g. after CSI C).
        while (line.length < cursorCol) line.append(' ')
        if (cursorCol < line.length) {
            line.setCharAt(cursorCol, c)
        } else {
            line.append(c)
        }
        cursorCol++
    }

    private fun newLine() {
        lines.add(StringBuilder())
        cursorCol = 0
        // Bound memory: drop the oldest line(s) once we exceed the cap.
        while (lines.size > maxLines) lines.removeAt(0)
    }
}
