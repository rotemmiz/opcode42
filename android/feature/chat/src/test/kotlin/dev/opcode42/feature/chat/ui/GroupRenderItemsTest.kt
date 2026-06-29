package dev.opcode42.feature.chat.ui

import dev.opcode42.core.model.*
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

private fun toolPart(
    id: String,
    tool: String,
    filePath: String? = null,
    msgID: String = "m1",
): ToolPart {
    val input = if (filePath != null) buildJsonObject { put("filePath", filePath) } else null
    return ToolPart(
        id = id,
        sessionID = "s1",
        messageID = msgID,
        callID = id,
        tool = tool,
        state = ToolStateCompleted(input = input),
    )
}

private fun patchPart(id: String, files: List<String>, msgID: String = "m1") =
    PatchPart(id = id, sessionID = "s1", messageID = msgID, hash = "abc1234", files = files)

private fun textPart(id: String, msgID: String = "m1") =
    TextPart(id = id, sessionID = "s1", messageID = msgID, text = "hi")

class GroupRenderItemsTest {

    @Test
    fun emptyParts_returnsEmpty() {
        assertEquals(emptyList<RenderItem>(), groupRenderItems(emptyList()))
    }

    @Test
    fun nonToolParts_becomeSingle() {
        val tp = textPart("p1")
        val items = groupRenderItems(listOf(tp))
        assertEquals(1, items.size)
        assertTrue(items[0] is RenderItem.Single)
        assertEquals(tp, (items[0] as RenderItem.Single).part)
    }

    @Test
    fun consecutiveToolParts_groupedIntoTools() {
        val parts: List<Part> = listOf(
            toolPart("t1", "read"),
            toolPart("t2", "read"),
        )
        val items = groupRenderItems(parts)
        assertEquals(1, items.size)
        val group = items[0] as RenderItem.Tools
        assertEquals(2, group.parts.size)
    }

    @Test
    fun writeToolPart_rendersAsOwnBlock() {
        val parts: List<Part> = listOf(
            toolPart("t1", "read"),
            toolPart("t2", "write", filePath = "foo.kt"),
            toolPart("t3", "read"),
        )
        val items = groupRenderItems(parts)
        // read, write (single), read
        assertEquals(3, items.size)
        assertTrue(items[0] is RenderItem.Tools)
        assertTrue(items[1] is RenderItem.Single)
        assertTrue(items[2] is RenderItem.Tools)
    }

    @Test
    fun todoWritePart_isHiddenFromRows() {
        val parts: List<Part> = listOf(
            toolPart("t1", "todowrite"),
            toolPart("t2", "read"),
        )
        val items = groupRenderItems(parts)
        assertEquals(1, items.size)
        val group = items[0] as RenderItem.Tools
        assertEquals(1, group.parts.size)
        assertEquals("t2", group.parts[0].id)
    }

    @Test
    fun patchPart_becomesRenderItemPatch() {
        val edit = toolPart("t1", "edit", filePath = "src/Foo.kt")
        val patch = patchPart("p1", files = listOf("src/Foo.kt"))
        val items = groupRenderItems(listOf(edit, patch))
        val patchItem = items.filterIsInstance<RenderItem.Patch>()
        assertEquals(1, patchItem.size)
        assertEquals(patch, patchItem[0].part)
    }

    @Test
    fun patchPart_associatesMatchingEditParts() {
        val edit = toolPart("t1", "edit", filePath = "src/Foo.kt")
        val patch = patchPart("p1", files = listOf("src/Foo.kt"))
        val items = groupRenderItems(listOf(edit, patch))
        val patchItem = items.filterIsInstance<RenderItem.Patch>().first()
        assertEquals(listOf(edit), patchItem.editParts)
    }

    @Test
    fun patchPart_claimedEditsExcludedFromToolRowGroup() {
        val edit = toolPart("t1", "edit", filePath = "src/Foo.kt")
        val read = toolPart("t2", "read")
        val patch = patchPart("p1", files = listOf("src/Foo.kt"))
        val items = groupRenderItems(listOf(read, edit, patch))
        val groups = items.filterIsInstance<RenderItem.Tools>()
        // Only the read ToolPart remains in the group; the edit is claimed by the patch
        assertEquals(1, groups.size)
        assertEquals(listOf("t2"), groups[0].parts.map { it.id })
    }

    @Test
    fun multipleEditsToSameFile_allClaimed() {
        val edit1 = toolPart("t1", "edit", filePath = "src/Foo.kt")
        val edit2 = toolPart("t2", "edit", filePath = "src/Foo.kt")
        val patch = patchPart("p1", files = listOf("src/Foo.kt"))
        val items = groupRenderItems(listOf(edit1, edit2, patch))
        val patchItem = items.filterIsInstance<RenderItem.Patch>().first()
        assertEquals(listOf(edit1, edit2), patchItem.editParts)
        assertEquals(0, items.filterIsInstance<RenderItem.Tools>().size)
    }

    @Test
    fun patchPartWithNoMatchingEdits_getsEmptyEditParts() {
        val patch = patchPart("p1", files = listOf("src/Bar.kt"))
        val items = groupRenderItems(listOf(patch))
        val patchItem = items.filterIsInstance<RenderItem.Patch>().first()
        assertEquals(emptyList<ToolPart>(), patchItem.editParts)
    }
}
