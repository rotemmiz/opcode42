package dev.opcode42.feature.sessions.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.design.brand.Spinner
import dev.opcode42.core.design.theme.Secondary
import dev.opcode42.core.model.PermissionRequest
import dev.opcode42.core.model.QuestionRequest

/**
 * A session is "in flight" when the daemon reports any non-idle status for it
 * (opencode emits `session.status` with `type: "busy"` while a turn runs).
 */
fun isSessionBusy(status: String?): Boolean = status != null && status != "idle"

/**
 * 12dp spinner shown beside a session's title while a turn is in flight — the
 * per-session activity indicator surfaced in the sessions menu. Renders nothing
 * when the session is idle; shown for any non-idle status (the daemon emits
 * `type: "busy"` today).
 */
@Composable
fun SessionStatusSpinner(status: String?, modifier: Modifier = Modifier) {
    // Always composed so it fades out (1→0) when the session goes idle, not just in.
    Spinner(visible = isSessionBusy(status), modifier = modifier, color = Secondary)
}

/**
 * Inline, in-menu affordance for a session that needs the user — surfaced directly
 * in the sessions list so it can be answered without opening that session:
 *  - a pending **permission** renders compact Approve / Deny buttons,
 *  - a pending **question** with options renders each option label as a quick
 *    single-tap reply (single-select) or toggle chips + Submit (multi-select);
 *    a no-options / `custom`-only question keeps the free-text field + Reply / Skip.
 *
 * Renders nothing when neither is present. Callers place this below the row's
 * clickable title so tapping a button doesn't also open the session.
 */
@Composable
fun SessionPendingActions(
    permission: PermissionRequest?,
    question: QuestionRequest?,
    onApprove: () -> Unit,
    onDeny: () -> Unit,
    onReply: (List<List<String>>) -> Unit,
    onSkip: () -> Unit,
    modifier: Modifier = Modifier,
    isReplying: Boolean = false,
) {
    val buttonPadding = PaddingValues(horizontal = 8.dp, vertical = 0.dp)
    when {
        permission != null -> {
            Column(modifier.fillMaxWidth()) {
                Text(
                    text = permission.title ?: "Permission requested",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                permission.description?.let { desc ->
                    Text(
                        text = desc,
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                Spacer(Modifier.height(6.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(
                        onClick = onDeny,
                        modifier = Modifier.weight(1f).heightIn(min = 34.dp),
                        contentPadding = buttonPadding,
                        enabled = !isReplying,
                        colors = ButtonDefaults.outlinedButtonColors(
                            contentColor = MaterialTheme.colorScheme.error,
                        ),
                    ) {
                        if (isReplying) {
                            CircularProgressIndicator(
                                modifier = Modifier.size(14.dp),
                                strokeWidth = 2.dp,
                                color = MaterialTheme.colorScheme.error,
                            )
                        } else {
                            Text("Deny", fontSize = 13.sp)
                        }
                    }
                    Button(
                        onClick = onApprove,
                        modifier = Modifier.weight(1f).heightIn(min = 34.dp),
                        contentPadding = buttonPadding,
                        enabled = !isReplying,
                    ) {
                        if (isReplying) {
                            CircularProgressIndicator(
                                modifier = Modifier.size(14.dp),
                                strokeWidth = 2.dp,
                                color = MaterialTheme.colorScheme.onPrimary,
                            )
                        } else {
                            Text("Approve", fontSize = 13.sp)
                        }
                    }
                }
            }
        }
        question != null -> {
            val info = question.questions.firstOrNull()
            if (info == null) {
                // Degenerate: no structured questions. Keep a free-text fallback so the
                // user can still answer from the menu.
                FreeTextQuestionActions(
                    label = question.message ?: "The agent has a question",
                    modifier = modifier,
                    buttonPadding = buttonPadding,
                    onReply = onReply,
                    onSkip = onSkip,
                    isReplying = isReplying,
                )
                return
            }
            if (info.options.isNotEmpty()) {
                StructuredQuestionActions(
                    question = question,
                    info = info,
                    modifier = modifier,
                    buttonPadding = buttonPadding,
                    onReply = onReply,
                    onSkip = onSkip,
                    isReplying = isReplying,
                )
            } else {
                // No-options / custom-only: free-text field + Reply / Skip.
                FreeTextQuestionActions(
                    label = info.question.ifBlank { "The agent has a question" },
                    modifier = modifier,
                    buttonPadding = buttonPadding,
                    onReply = onReply,
                    onSkip = onSkip,
                    isReplying = isReplying,
                )
            }
        }
    }
}

/**
 * Structured question with option labels: single-select renders each label as a
 * one-tap quick reply; multi-select renders toggle chips + Submit (and Skip).
 * The menu only handles the first question; a multi-question request shows a
 * "N questions — open session to answer all" hint above the first question's
 * options so the user knows to open the session for the rest.
 */
@Composable
private fun StructuredQuestionActions(
    question: QuestionRequest,
    info: dev.opcode42.core.model.QuestionInfo,
    modifier: Modifier,
    buttonPadding: PaddingValues,
    onReply: (List<List<String>>) -> Unit,
    onSkip: () -> Unit,
    isReplying: Boolean,
) {
    val multiple = info.multiple == true
    val multiQuestion = question.questions.size > 1
    // Per-tap selections for multi-select. A SnapshotStateList (observed on add/remove) so
    // toggling a chip recomposes the chips + the Submit enable state. Keyed by the request
    // id + question text so a re-shown sheet for a new request starts fresh.
    val selected = remember(question.id, info.question) { mutableStateListOf<String>() }
    Column(modifier.fillMaxWidth()) {
        if (multiQuestion) {
            Text(
                text = "${question.questions.size} questions — open session to answer all",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.height(4.dp))
        }
        Text(
            text = info.question.ifBlank { "The agent has a question" },
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurface,
            maxLines = 3,
            overflow = TextOverflow.Ellipsis,
        )
        Spacer(Modifier.height(6.dp))
        if (multiple) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                info.options.forEach { opt ->
                    FilterChip(
                        selected = opt.label in selected,
                        onClick = {
                            if (isReplying) return@FilterChip
                            if (opt.label in selected) {
                                selected.remove(opt.label)
                            } else {
                                selected.add(opt.label)
                            }
                        },
                        enabled = !isReplying,
                        label = { Text(opt.label, fontSize = 12.5.sp, maxLines = 1) },
                    )
                }
            }
            Spacer(Modifier.height(6.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(
                    onClick = onSkip,
                    modifier = Modifier.weight(1f).heightIn(min = 34.dp),
                    contentPadding = buttonPadding,
                    enabled = !isReplying,
                ) { Text("Skip", fontSize = 13.sp) }
                Button(
                    onClick = { onReply(menuMultiSelectSubmitReply(selected.toList())) },
                    modifier = Modifier.weight(1f).heightIn(min = 34.dp),
                    contentPadding = buttonPadding,
                    enabled = !isReplying && selected.isNotEmpty(),
                ) {
                    if (isReplying) {
                        CircularProgressIndicator(
                            modifier = Modifier.size(14.dp),
                            strokeWidth = 2.dp,
                            color = MaterialTheme.colorScheme.onPrimary,
                        )
                    } else {
                        Text("Submit", fontSize = 13.sp)
                    }
                }
            }
        } else {
            info.options.forEach { opt ->
                OutlinedButton(
                    onClick = { onReply(menuSingleSelectReply(opt.label)) },
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = 34.dp),
                    contentPadding = buttonPadding,
                    enabled = !isReplying,
                ) { Text(opt.label, fontSize = 13.sp, maxLines = 1) }
                Spacer(Modifier.height(4.dp))
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(
                    onClick = onSkip,
                    modifier = Modifier.weight(1f).heightIn(min = 34.dp),
                    contentPadding = buttonPadding,
                    enabled = !isReplying,
                ) {
                    if (isReplying) {
                        CircularProgressIndicator(
                            modifier = Modifier.size(14.dp),
                            strokeWidth = 2.dp,
                            color = MaterialTheme.colorScheme.error,
                        )
                    } else {
                        Text("Skip", fontSize = 13.sp)
                    }
                }
            }
        }
    }
}

/**
 * Free-text question (no options / custom-only / degenerate): an `OutlinedTextField`
 * + Reply / Skip. `rememberSaveable` keyed by the label so a half-typed answer
 * survives the row scrolling out of the LazyColumn.
 */
@Composable
private fun FreeTextQuestionActions(
    label: String,
    modifier: Modifier,
    buttonPadding: PaddingValues,
    onReply: (List<List<String>>) -> Unit,
    onSkip: () -> Unit,
    isReplying: Boolean,
) {
    var answer by rememberSaveable(label) { mutableStateOf("") }
    Column(modifier.fillMaxWidth()) {
        Text(
            text = label,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurface,
            maxLines = 3,
            overflow = TextOverflow.Ellipsis,
        )
        Spacer(Modifier.height(6.dp))
        OutlinedTextField(
            value = answer,
            onValueChange = { answer = it },
            placeholder = { Text("Your answer", fontSize = 13.sp) },
            textStyle = MaterialTheme.typography.bodyMedium,
            modifier = Modifier.fillMaxWidth(),
            minLines = 1,
            maxLines = 3,
            enabled = !isReplying,
        )
        Spacer(Modifier.height(6.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(
                onClick = onSkip,
                modifier = Modifier.weight(1f).heightIn(min = 34.dp),
                contentPadding = buttonPadding,
                enabled = !isReplying,
            ) { Text("Skip", fontSize = 13.sp) }
            Button(
                onClick = { onReply(menuFreeTextReply(answer)) },
                modifier = Modifier.weight(1f).heightIn(min = 34.dp),
                contentPadding = buttonPadding,
                enabled = !isReplying && answer.isNotBlank(),
            ) {
                if (isReplying) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(14.dp),
                        strokeWidth = 2.dp,
                        color = MaterialTheme.colorScheme.onPrimary,
                    )
                } else {
                    Text("Reply", fontSize = 13.sp)
                }
            }
        }
    }
}

/**
 * Build the `List<List<String>>` answers shape the wire contract expects
 * (`{ answers: string[][] }`) from the menu's quick reply. The menu only answers
 * the first question, so the outer list has exactly one inner list of labels.
 * Pure so it can be unit-tested without a Compose test rule.
 */
internal fun buildMenuQuestionReply(labels: List<String>): List<List<String>> = listOf(labels)

/**
 * The menu's reply for a single-select option tap — one label, wrapped as a
 * single-element outer list. Pure so the single-tap quick-reply behavior is
 * unit-testable.
 */
internal fun menuSingleSelectReply(label: String): List<List<String>> = buildMenuQuestionReply(listOf(label))

/**
 * The menu's reply for a multi-select Submit — the selected labels (in the order
 * the user toggled them), wrapped as a single-element outer list. Pure so the
 * multi-select submit behavior is unit-testable. Returns null when nothing is
 * selected (Submit is disabled in that case; this lets a test assert "no reply
 * fired").
 */
internal fun menuMultiSelectReply(selected: List<String>): List<List<String>>? =
    if (selected.isEmpty()) null else buildMenuQuestionReply(selected)

/**
 * The non-null submit reply — the call site's Submit button is gated on
 * `selected.isNotEmpty()`, so this never sees an empty list. Kept separate from
 * [menuMultiSelectReply] so the composable never needs `!!` and the empty→null
 * contract stays unit-testable.
 */
internal fun menuMultiSelectSubmitReply(selected: List<String>): List<List<String>> =
    buildMenuQuestionReply(selected)

/**
 * The menu's reply for the free-text field — the typed text, wrapped as a
 * single-element outer list. The caller gates on `text.isNotBlank()` (the Reply
 * button is disabled when blank). Pure so the free-text reply behavior is
 * unit-testable.
 */
internal fun menuFreeTextReply(text: String): List<List<String>> = buildMenuQuestionReply(listOf(text))
