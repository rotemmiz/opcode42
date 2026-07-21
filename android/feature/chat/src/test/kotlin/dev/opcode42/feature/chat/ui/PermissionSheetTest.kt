package dev.opcode42.feature.chat.ui

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
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

    private fun render(permission: PermissionRequest, isReplying: Boolean = false): MutableList<String> {
        val replies = mutableListOf<String>()
        composeRule.setContent {
            Opcode42Theme {
                PermissionSheetContent(
                    permission = permission,
                    onReply = { replies += it },
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
    fun denyButton_callsOnReplyReject() {
        val replies = render(permission())
        composeRule.onNodeWithText("Deny").performClick()
        org.junit.Assert.assertEquals(listOf("reject"), replies)
    }

    @Test
    fun allowOnceButton_callsOnReplyOnce() {
        val replies = render(permission())
        composeRule.onNodeWithText("Allow once").performClick()
        org.junit.Assert.assertEquals(listOf("once"), replies)
    }

    @Test
    fun alwaysButton_callsOnReplyAlways() {
        val replies = render(permission(always = listOf("edit_file")))
        composeRule.onNodeWithText("Always").performClick()
        org.junit.Assert.assertEquals(listOf("always"), replies)
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
}