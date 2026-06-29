package dev.opcode42.app.ui

import androidx.window.core.layout.WindowHeightSizeClass
import androidx.window.core.layout.WindowWidthSizeClass
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Truth table for [chatLayoutFor] — one case per row of the layout matrix in
 * plans/07-client-mobile.md, plus the folded-cover and tiny-split-screen edges.
 * The rule keys on width: compact width → single pane + overlay menu + no right panel;
 * any wider window → right panel + inline-push menu.
 */
class ChatLayoutTest {

    @Test fun phonePortrait_singlePane_overlay_noRightPanel() {
        val l = chatLayoutFor(WindowWidthSizeClass.COMPACT, WindowHeightSizeClass.MEDIUM)
        assertTrue(l.singlePane)
        assertFalse(l.showRightPanel)
        assertEquals(LeftRailMode.Overlay, l.leftRailMode)
    }

    @Test fun phoneLandscape_rightPanel_inlinePush() {
        // Wide but short — typical phone in landscape.
        val l = chatLayoutFor(WindowWidthSizeClass.EXPANDED, WindowHeightSizeClass.COMPACT)
        assertFalse(l.singlePane)
        assertTrue(l.showRightPanel)
        assertEquals(LeftRailMode.InlinePush, l.leftRailMode)
    }

    @Test fun foldablePortrait_rightPanel_inlinePush() {
        val l = chatLayoutFor(WindowWidthSizeClass.MEDIUM, WindowHeightSizeClass.MEDIUM)
        assertFalse(l.singlePane)
        assertTrue(l.showRightPanel)
        assertEquals(LeftRailMode.InlinePush, l.leftRailMode)
    }

    @Test fun foldableLandscape_rightPanel_inlinePush() {
        val l = chatLayoutFor(WindowWidthSizeClass.EXPANDED, WindowHeightSizeClass.EXPANDED)
        assertFalse(l.singlePane)
        assertTrue(l.showRightPanel)
        assertEquals(LeftRailMode.InlinePush, l.leftRailMode)
    }

    @Test fun tablet_anyOrientation_rightPanel_inlinePush() {
        val portrait = chatLayoutFor(WindowWidthSizeClass.MEDIUM, WindowHeightSizeClass.EXPANDED)
        val landscape = chatLayoutFor(WindowWidthSizeClass.EXPANDED, WindowHeightSizeClass.MEDIUM)
        for (l in listOf(portrait, landscape)) {
            assertTrue(l.showRightPanel)
            assertEquals(LeftRailMode.InlinePush, l.leftRailMode)
        }
    }

    @Test fun foldedCover_compactWidth_behavesLikePhone() {
        // Tall narrow cover display → compact width → single pane / overlay.
        val l = chatLayoutFor(WindowWidthSizeClass.COMPACT, WindowHeightSizeClass.EXPANDED)
        assertTrue(l.singlePane)
        assertEquals(LeftRailMode.Overlay, l.leftRailMode)
    }

    @Test fun tinySplitScreen_bothCompact_isSinglePane() {
        val l = chatLayoutFor(WindowWidthSizeClass.COMPACT, WindowHeightSizeClass.COMPACT)
        assertTrue(l.singlePane)
        assertFalse(l.showRightPanel)
        assertEquals(LeftRailMode.Overlay, l.leftRailMode)
    }
}
