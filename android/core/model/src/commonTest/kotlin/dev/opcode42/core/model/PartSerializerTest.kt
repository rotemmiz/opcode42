package dev.opcode42.core.model

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs

/**
 * The model serializer must degrade a malformed part to UnknownPart instead of
 * throwing, so one bad part can't abort the whole session decode (see the file-
 * attachment crash). A well-formed file part must still decode to FilePart.
 */
class PartSerializerTest {

    @Test
    fun filePartMissingUrl_decodesAsUnknownPartNotThrow() {
        // `url` is required on FilePart and is absent here.
        val json = """
            {"type":"file","id":"p1","sessionID":"s1","messageID":"m1","mime":"image/png"}
        """.trimIndent()

        val part = Opcode42Json.decodeFromString(PartSerializer, json)

        val unknown = assertIs<UnknownPart>(part)
        assertEquals("file", unknown.type)
        assertEquals("p1", unknown.id)
        assertEquals("s1", unknown.sessionID)
        assertEquals("m1", unknown.messageID)
    }

    @Test
    fun wellFormedFilePart_decodesAsFilePart() {
        val json = """
            {"type":"file","id":"p1","sessionID":"s1","messageID":"m1",
             "mime":"image/png","filename":"shot.png","url":"data:image/png;base64,AAAA"}
        """.trimIndent()

        val part = Opcode42Json.decodeFromString(PartSerializer, json)

        val file = assertIs<FilePart>(part)
        assertEquals("image/png", file.mime)
        assertEquals("shot.png", file.filename)
        assertEquals("data:image/png;base64,AAAA", file.url)
    }
}
