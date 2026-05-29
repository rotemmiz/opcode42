// Package pty spawns shells and bridges them to the WebSocket-PTY transport
// with an output ring buffer.
//
// Framing matches opencode (pty/index.ts:17-18,44-52,239-262,301-361):
//   - 2MB buffer limit, 64KB chunks.
//   - Server control frame: byte 0x00 followed by UTF-8 JSON {"cursor":<n>}.
//   - Data frames: plain UTF-8 in <=64KB slices.
//   - cursor is a UTF-16 code-unit count (session.cursor += chunk.length on a
//     JS string), NOT a byte or rune count — Go must count UTF-16 code units.
//   - ?cursor=-1 means start at the current end.
//
// Implemented in plan 01 (M5).
package pty
