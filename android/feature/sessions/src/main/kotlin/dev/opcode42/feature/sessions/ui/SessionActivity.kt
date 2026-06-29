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
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
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
    if (isSessionBusy(status)) {
        CircularProgressIndicator(
            modifier = modifier.size(12.dp),
            strokeWidth = 1.5.dp,
            color = MaterialTheme.colorScheme.secondary,
        )
    }
}

/**
 * Inline, in-menu affordance for a session that needs the user — surfaced directly
 * in the sessions list so it can be answered without opening that session:
 *  - a pending **permission** renders compact Approve / Deny buttons,
 *  - a pending **question** (free-text) renders a small text field + Reply / Skip.
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
    onReply: (String) -> Unit,
    onSkip: () -> Unit,
    modifier: Modifier = Modifier,
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
                        colors = ButtonDefaults.outlinedButtonColors(
                            contentColor = MaterialTheme.colorScheme.error,
                        ),
                    ) { Text("Deny", fontSize = 13.sp) }
                    Button(
                        onClick = onApprove,
                        modifier = Modifier.weight(1f).heightIn(min = 34.dp),
                        contentPadding = buttonPadding,
                    ) { Text("Approve", fontSize = 13.sp) }
                }
            }
        }
        question != null -> {
            // rememberSaveable so a half-typed answer survives the row scrolling out of
            // the LazyColumn (or a config change) — the menu is the point of answering here.
            var answer by rememberSaveable(question.id) { mutableStateOf("") }
            Column(modifier.fillMaxWidth()) {
                Text(
                    text = question.message ?: "The agent has a question",
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
                )
                Spacer(Modifier.height(6.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(
                        onClick = onSkip,
                        modifier = Modifier.weight(1f).heightIn(min = 34.dp),
                        contentPadding = buttonPadding,
                    ) { Text("Skip", fontSize = 13.sp) }
                    Button(
                        onClick = { onReply(answer) },
                        modifier = Modifier.weight(1f).heightIn(min = 34.dp),
                        contentPadding = buttonPadding,
                        enabled = answer.isNotBlank(),
                    ) { Text("Reply", fontSize = 13.sp) }
                }
            }
        }
    }
}
