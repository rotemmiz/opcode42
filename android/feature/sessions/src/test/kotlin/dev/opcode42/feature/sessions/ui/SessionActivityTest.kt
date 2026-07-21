package dev.opcode42.feature.sessions.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * PR 1.5 — the side menu renders a pending question's option labels (not a free-text
 * field) for a quick single-tap reply; multi-select gets toggle chips + Submit; the
 * no-options / `custom`-only edge case keeps the free-text field. These tests cover
 * the pure reply builders the composable calls on each interaction — the module has
 * no Compose UI test rule, so the wire-shape behavior is unit-tested directly:
 *  - single-select: tapping one option → `[[label]]`;
 *  - multi-select: Submit with two toggled → `[[a, b]]`;
 *  - no-options: free-text Reply → `[[typed text]]`;
 *  - Skip is handled by the caller (onSkip) and is not a reply shape.
 */
class SessionActivityTest {

    @Test
    fun singleSelect_tapOneOption_repliesThatLabelOnly() {
        val options = listOf("opt1", "opt2", "opt3")
        // The composable renders each label as a one-tap button that calls
        // menuSingleSelectReply(label); tapping "opt2" produces [[opt2]].
        val reply = menuSingleSelectReply(options[1])
        assertEquals(listOf(listOf("opt2")), reply)
    }

    @Test
    fun multiSelect_toggleTwoAndSubmit_repliesBothLabels() {
        // The composable toggles chips into a set, then Submit calls
        // menuMultiSelectReply(selected). Toggling "a" and "b" → [[a, b]].
        val selected = listOf("a", "b")
        val reply = menuMultiSelectReply(selected)
        assertEquals(listOf(listOf("a", "b")), reply)
    }

    @Test
    fun multiSelect_submitWithNothingSelected_repliesNull() {
        // Submit is disabled when nothing is selected; the builder returns null so a
        // test can assert "no reply fired" rather than sending an empty answer.
        val reply = menuMultiSelectReply(emptyList())
        assertNull(reply)
    }

    @Test
    fun noOptions_freeTextReply_repliesTheTypedText() {
        // The no-options / custom-only branch keeps the free-text field; Reply calls
        // menuFreeTextReply(text). Typing "typed text" → [[typed text]].
        val reply = menuFreeTextReply("typed text")
        assertEquals(listOf(listOf("typed text")), reply)
    }

    @Test
    fun singleSelect_replyHasExactlyOneInnerList() {
        // The menu only answers the first question, so the outer list has length 1.
        val reply = menuSingleSelectReply("only")
        assertEquals(1, reply.size)
        assertEquals(listOf("only"), reply[0])
    }

    @Test
    fun multiSelect_preservesSelectionOrder() {
        // The selected set is backed by a MutableSet; the reply preserves the order the
        // labels were added (the user's toggle order), which is what the daemon receives.
        val reply = menuMultiSelectReply(listOf("second", "first"))
        assertEquals(listOf(listOf("second", "first")), reply)
    }
}