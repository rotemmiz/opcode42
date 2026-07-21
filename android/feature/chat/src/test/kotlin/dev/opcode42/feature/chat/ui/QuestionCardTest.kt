package dev.opcode42.feature.chat.ui

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import dev.opcode42.core.design.theme.Opcode42Theme
import dev.opcode42.core.model.QuestionInfo
import dev.opcode42.core.model.QuestionOption
import dev.opcode42.core.model.QuestionRequest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33], qualifiers = "w360dp-h760dp")
class QuestionCardTest {

    @get:Rule val composeRule = createComposeRule()

    private fun question(
        questions: List<QuestionInfo>,
        id: String = "q_1",
    ) = QuestionRequest(id = id, sessionID = "ses_1", questions = questions)

    private fun singleSelect(opts: List<String>): QuestionInfo = QuestionInfo(
        question = "Pick one",
        header = "Choice",
        options = opts.map { QuestionOption(label = it) },
        multiple = false,
        custom = false,
    )

    private fun multiSelect(opts: List<String>): QuestionInfo = QuestionInfo(
        question = "Pick many",
        header = "Choices",
        options = opts.map { QuestionOption(label = it) },
        multiple = true,
        custom = false,
    )

    private fun render(
        question: QuestionRequest,
        resolvedAnswers: List<List<String>>? = null,
        resolvedSkipped: Boolean = false,
        isReplying: Boolean = false,
    ): Pair<MutableList<List<List<String>>>, MutableList<Unit>> {
        val replies = mutableListOf<List<List<String>>>()
        val rejects = mutableListOf<Unit>()
        composeRule.setContent {
            Opcode42Theme {
                QuestionCard(
                    question = question,
                    resolvedAnswers = resolvedAnswers,
                    resolvedSkipped = resolvedSkipped,
                    onReply = { replies += it },
                    onReject = { rejects += Unit },
                    isReplying = isReplying,
                )
            }
        }
        return replies to rejects
    }

    @Test
    fun pending_rendersHeaderQuestionAndOptions() {
        render(question(listOf(singleSelect(listOf("opt1", "opt2", "opt3")))))
        composeRule.onNodeWithText("Choice").assertIsDisplayed()
        composeRule.onNodeWithText("Pick one").assertIsDisplayed()
        composeRule.onNodeWithText("opt1").assertIsDisplayed()
        composeRule.onNodeWithText("opt2").assertIsDisplayed()
        composeRule.onNodeWithText("opt3").assertIsDisplayed()
        composeRule.onNodeWithText("Submit").assertIsDisplayed()
        composeRule.onNodeWithText("Skip").assertIsDisplayed()
    }

    @Test
    fun singleSelect_tappingOption_deselectsOthers() {
        val (replies, _) = render(question(listOf(singleSelect(listOf("opt1", "opt2")))))
        // Select opt1 then opt2 — only opt2 should be in the submitted answer.
        composeRule.onNodeWithText("opt1").performClick()
        composeRule.onNodeWithText("opt2").performClick()
        composeRule.onNodeWithText("Submit").performClick()
        assertEquals(listOf(listOf(listOf("opt2"))), replies)
    }

    @Test
    fun multiSelect_togglesIndependently() {
        val (replies, _) = render(question(listOf(multiSelect(listOf("a", "b", "c")))))
        composeRule.onNodeWithText("a").performClick()
        composeRule.onNodeWithText("b").performClick()
        composeRule.onNodeWithText("Submit").performClick()
        assertEquals(listOf(listOf(listOf("a", "b"))), replies)
    }

    @Test
    fun skip_callsOnReject() {
        val (_, rejects) = render(question(listOf(singleSelect(listOf("opt1")))))
        composeRule.onNodeWithText("Skip").performClick()
        assertEquals(1, rejects.size)
    }

    @Test
    fun resolvedAnswers_rendersHistoryRow_notTappable() {
        val (replies, rejects) = render(
            question(listOf(singleSelect(listOf("opt1")))),
            resolvedAnswers = listOf(listOf("opt1")),
        )
        composeRule.onNodeWithText("Answered: opt1").assertIsDisplayed()
        // Pending UI must not be present.
        composeRule.onNodeWithText("Submit").assertIsNotDisplayed()
        composeRule.onNodeWithText("Skip").assertIsNotDisplayed()
        assertEquals(0, replies.size)
        assertEquals(0, rejects.size)
    }

    @Test
    fun resolvedSkipped_rendersSkippedRow() {
        val (replies, rejects) = render(
            question(listOf(singleSelect(listOf("opt1")))),
            resolvedSkipped = true,
        )
        composeRule.onNodeWithText("Skipped").assertIsDisplayed()
        composeRule.onNodeWithText("Submit").assertIsNotDisplayed()
        assertNull(replies.firstOrNull())
        assertEquals(0, rejects.size)
    }
}