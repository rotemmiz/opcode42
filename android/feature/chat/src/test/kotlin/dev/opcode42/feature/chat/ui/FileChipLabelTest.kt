package dev.opcode42.feature.chat.ui

import dev.opcode42.core.model.FilePart
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Guards the actual crash fix: a file attachment is stored as a base64 `data:` URL in
 * `FilePart.url`, and rendering that raw URL as a Text label OOM-killed the app. The chip
 * label must never return a multi-megabyte string.
 */
class FileChipLabelTest {

    private fun filePart(mime: String = "image/png", filename: String? = null, url: String) =
        FilePart(id = "p1", sessionID = "s1", messageID = "m1", mime = mime, filename = filename, url = url)

    @Test
    fun dataUrlWithoutFilename_returnsMimeNotRawUrl() {
        // The exact OOM trigger: a huge data URL with no display name.
        val hugeDataUrl = "data:video/mp4;base64," + "A".repeat(5_000_000)
        val label = fileChipLabel(filePart(mime = "video/mp4", filename = null, url = hugeDataUrl))

        assertEquals("video/mp4", label)
        assertTrue("label must stay short, never the raw URL", label.length <= 120)
    }

    @Test
    fun dataUrlCaseInsensitive_returnsMime() {
        val label = fileChipLabel(filePart(mime = "image/png", url = "DATA:image/png;base64,AAAA"))
        assertEquals("image/png", label)
    }

    @Test
    fun dataUrlWithBlankMime_fallsBackToAttachment() {
        val label = fileChipLabel(filePart(mime = "", url = "data:;base64,AAAA"))
        assertEquals("attachment", label)
    }

    @Test
    fun filenamePresent_returnsFilename() {
        val label = fileChipLabel(filePart(filename = "shot.png", url = "data:image/png;base64,AAAA"))
        assertEquals("shot.png", label)
    }

    @Test
    fun blankFilename_fallsThroughToMime() {
        val label = fileChipLabel(filePart(mime = "image/png", filename = "  ", url = "data:image/png;base64,AAAA"))
        assertEquals("image/png", label)
    }

    @Test
    fun pathologicallyLongFilename_isCappedAt120() {
        val label = fileChipLabel(filePart(filename = "x".repeat(10_000), url = "data:image/png;base64,AAAA"))
        assertEquals(120, label.length)
    }

    @Test
    fun nonDataUrl_returnsLastPathSegment() {
        val label = fileChipLabel(filePart(url = "https://example.com/dir/report.pdf"))
        assertEquals("report.pdf", label)
    }
}
