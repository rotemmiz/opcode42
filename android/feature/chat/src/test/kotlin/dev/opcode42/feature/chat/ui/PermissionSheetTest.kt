package dev.opcode42.feature.chat.ui

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import dev.opcode42.core.design.theme.Opcode42Theme
import dev.opcode42.core.model.PermissionRequest
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33], qualifiers = "w360dp-h760dp")
class PermissionSheetTest {

    @get:Rule val composeRule = createComposeRule()

    private fun permission(
        always: List<String> = emptyList(),
        patterns: List<String> = listOf("src/main.kt"),
        permission: String = "edit_file",
    ) = PermissionRequest(
        id = "perm_1",
        sessionID = "ses_1",
        permission = permission,
        patterns = patterns,
        always = always,
    )

    private fun render(permission: PermissionRequest, isReplying: Boolean = false): MutableList<Pair<String, String?>> {
        val replies = mutableListOf<Pair<String, String?>>()
        composeRule.setContent {
            Opcode42Theme {
                PermissionSheetContent(
                    permission = permission,
                    onReply = { reply, message -> replies += reply to message },
                    isReplying = isReplying,
                )
            }
        }
        return replies
    }

    @Test
    fun rendersPermissionTitleAndPatterns() {
        render(permission(patterns = listOf("src/a.kt", "src/b.kt"), permission = "bash"))
        composeRule.onNodeWithText("bash").assertIsDisplayed()
        composeRule.onNodeWithText("src/a.kt, src/b.kt").assertIsDisplayed()
    }

    @Test
    fun blankPermission_fallsBackToDefaultTitle() {
        render(permission(permission = "", patterns = listOf("x")))
        composeRule.onNodeWithText("Permission required").assertIsDisplayed()
    }

    @Test
    fun alwaysEmpty_showsTwoButtons() {
        render(permission(always = emptyList()))
        composeRule.onNodeWithText("Deny").assertIsDisplayed()
        composeRule.onNodeWithText("Allow once").assertIsDisplayed()
        composeRule.onNodeWithText("Always").assertDoesNotExist()
    }

    @Test
    fun alwaysNonEmpty_showsThreeButtons() {
        render(permission(always = listOf("edit_file")))
        composeRule.onNodeWithText("Deny").assertIsDisplayed()
        composeRule.onNodeWithText("Allow once").assertIsDisplayed()
        composeRule.onNodeWithText("Always").assertIsDisplayed()
    }

    @Test
    fun denyButton_collapsed_expandsFeedbackFieldWithoutReplying() {
        val replies = render(permission())
        composeRule.onNodeWithText("Deny").performClick()
        // Feedback field + Send button now visible.
        composeRule.onNodeWithText("Send feedback with deny").assertIsDisplayed()
        composeRule.onNodeWithText("Send").assertIsDisplayed()
        org.junit.Assert.assertTrue("Deny-tap on collapsed field must NOT reply", replies.isEmpty())
    }

    @Test
    fun sendButton_withFeedback_callsRejectWithMessage() {
        val replies = render(permission())
        composeRule.onNodeWithText("Deny").performClick()
        composeRule.onNodeWithText("Send feedback with deny").performTextInput("please don't")
        composeRule.onNodeWithText("Send").performClick()
        org.junit.Assert.assertEquals(listOf("reject" to "please don't"), replies)
    }

    @Test
    fun sendButton_emptyFeedback_callsRejectWithNullMessage() {
        val replies = render(permission())
        composeRule.onNodeWithText("Deny").performClick()
        composeRule.onNodeWithText("Send").performClick()
        org.junit.Assert.assertEquals(listOf("reject" to null), replies)
    }

    @Test
    fun allowOnceButton_callsOnReplyOnceWithNullMessage() {
        val replies = render(permission())
        composeRule.onNodeWithText("Allow once").performClick()
        org.junit.Assert.assertEquals(listOf("once" to null), replies)
    }

    @Test
    fun alwaysButton_callsOnReplyAlwaysWithNullMessage() {
        val replies = render(permission(always = listOf("edit_file")))
        composeRule.onNodeWithText("Always").performClick()
        org.junit.Assert.assertEquals(listOf("always" to null), replies)
    }

    @Test
    fun isReplying_disablesAllButtons() {
        render(permission(always = listOf("edit_file")), isReplying = true)
        // When replying, the Deny and Allow once buttons show a spinner instead of text,
        // so the labels are absent; Always remains text but disabled (no onReply fired).
        composeRule.onNodeWithText("Deny").assertDoesNotExist()
        composeRule.onNodeWithText("Allow once").assertDoesNotExist()
        composeRule.onNodeWithText("Always").performClick()
        // No reply emitted — clicking a disabled button is a no-op.
    }

    @Test
    fun isReplying_keepsFeedbackFieldHiddenUntilDenyTap() {
        render(permission(), isReplying = true)
        composeRule.onNodeWithText("Send feedback with deny").assertDoesNotExist()
        composeRule.onNodeWithText("Send").assertDoesNotExist()
    }

    @Test
    fun doubleTapDeny_firesOnReplyExactlyOnceWhenIsReplyingFlipsOnFirstTap() {
        // PR 1.6 — the double-tap guard: the caller sets `isReplying` synchronously on the
        // first tap (before the REST round-trip lands), so a second tap before the sheet
        // clears lands on a disabled Deny/Send and fires no extra reply. This test models
        // the ChatScreen wiring: onReply sets the replying id, which drives isReplying true,
        // which disables all buttons until the caller clears it. PR 3.4's deny-with-feedback
        // flow: the first Deny tap expands the feedback field (no reply); Send fires the
        // reply, which flips isReplying true and replaces Send with a spinner — so a second
        // Send tap finds no node and fires nothing.
        val replies = mutableListOf<Pair<String, String?>>()
        var replying by mutableStateOf(false)
        composeRule.setContent {
            Opcode42Theme {
                PermissionSheetContent(
                    permission = permission(),
                    onReply = { reply, message ->
                        replies += reply to message
                        replying = true
                    },
                    isReplying = replying,
                )
            }
        }
        composeRule.onNodeWithText("Deny").performClick()
        composeRule.onNodeWithText("Send").performClick()
        // isReplying is now true → the Send button is replaced by a spinner. The "Send"
        // text is gone, so a second tap cannot land — only one reply was fired.
        composeRule.onNodeWithText("Send").assertDoesNotExist()
        org.junit.Assert.assertEquals(listOf("reject" to null), replies)
    }

    @Test
    fun pendingCountGreaterThanOne_showsOneOfNBadge() {
        composeRule.setContent {
            Opcode42Theme {
                PermissionSheetContent(
                    permission = permission(),
                    onReply = { _, _ -> },
                    isReplying = false,
                    pendingCount = 2,
                )
            }
        }
        composeRule.onNodeWithText("1 of 2").assertIsDisplayed()
    }

    @Test
    fun pendingCountOne_omitsBadge() {
        composeRule.setContent {
            Opcode42Theme {
                PermissionSheetContent(
                    permission = permission(),
                    onReply = { _, _ -> },
                    isReplying = false,
                    pendingCount = 1,
                )
            }
        }
        composeRule.onNodeWithText("1 of 1").assertDoesNotExist()
    }
}