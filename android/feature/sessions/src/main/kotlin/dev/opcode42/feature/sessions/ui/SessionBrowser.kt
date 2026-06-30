package dev.opcode42.feature.sessions.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.design.theme.Opcode42Mono
import dev.opcode42.core.design.theme.OnSurfaceVariant
import dev.opcode42.core.design.theme.Secondary
import dev.opcode42.core.design.theme.Surface
import dev.opcode42.core.model.Session
import dev.opcode42.feature.sessions.SessionFilter
import dev.opcode42.feature.sessions.SessionListUiState

/**
 * Self-contained sessions browser shared by the full-screen list and the in-chat left rail:
 * a search field, status filter tabs (All / Working / Needs input with live counts), and a
 * date-grouped, recency-ordered `LazyColumn` of [SessionRow]s. Owns the rename dialog so any
 * surface gets rename for free. [compact] tightens it for the narrow (220dp) rail.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun SessionBrowser(
    uiState: SessionListUiState,
    activeSessionId: String?,
    onOpen: (Session) -> Unit,
    onQueryChange: (String) -> Unit,
    onFilterChange: (SessionFilter) -> Unit,
    onRename: (String, String) -> Unit,
    onArchive: (String) -> Unit,
    onFork: (String) -> Unit,
    onDelete: (String) -> Unit,
    onReplyPermission: (String, Boolean) -> Unit,
    onReplyQuestion: (String, String) -> Unit,
    onSkipQuestion: (String) -> Unit,
    modifier: Modifier = Modifier,
    compact: Boolean = false,
) {
    var renameTarget by remember { mutableStateOf<Session?>(null) }
    val hPad = if (compact) 8.dp else 12.dp

    Column(modifier.fillMaxSize()) {
        OutlinedTextField(
            value = uiState.query,
            onValueChange = onQueryChange,
            placeholder = { Text("Search", fontSize = if (compact) 13.sp else 15.sp) },
            leadingIcon = { Icon(Icons.Default.Search, contentDescription = null) },
            trailingIcon = {
                if (uiState.query.isNotEmpty()) {
                    IconButton(onClick = { onQueryChange("") }) {
                        Icon(Icons.Default.Close, contentDescription = "Clear search")
                    }
                }
            },
            singleLine = true,
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = hPad, vertical = 6.dp),
        )

        if (!uiState.showArchived) {
            FilterTabs(uiState, onFilterChange, compact, Modifier.padding(horizontal = hPad))
        }

        if (uiState.groups.isEmpty()) {
            Box(
                Modifier.fillMaxWidth().padding(24.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = if (uiState.query.isNotBlank() || uiState.filter != SessionFilter.All) {
                        "No matching sessions"
                    } else {
                        "No sessions"
                    },
                    color = OnSurfaceVariant,
                    fontSize = 13.sp,
                )
            }
        } else {
            LazyColumn(Modifier.fillMaxSize()) {
                uiState.groups.forEach { group ->
                    stickyHeader(key = "h:${group.header}") { DateHeader(group.header, hPad) }
                    items(group.sessions, key = { it.id }) { session ->
                        val permission = uiState.pendingPermissions[session.id]
                        val question = uiState.pendingQuestions[session.id]
                        SessionRow(
                            session = session,
                            isActive = session.id == activeSessionId,
                            status = uiState.statuses[session.id],
                            pendingPermission = permission,
                            pendingQuestion = question,
                            showArchived = uiState.showArchived,
                            onClick = { onOpen(session) },
                            onRename = { renameTarget = session },
                            onArchive = { onArchive(session.id) },
                            onFork = { onFork(session.id) },
                            onDelete = { onDelete(session.id) },
                            onApprove = { permission?.let { onReplyPermission(it.id, true) } },
                            onDeny = { permission?.let { onReplyPermission(it.id, false) } },
                            onReply = { answer -> question?.let { onReplyQuestion(it.id, answer) } },
                            onSkip = { question?.let { onSkipQuestion(it.id) } },
                            compact = compact,
                        )
                    }
                }
            }
        }
    }

    renameTarget?.let { session ->
        RenameSessionDialog(
            current = session.title,
            onConfirm = { title ->
                onRename(session.id, title)
                renameTarget = null
            },
            onDismiss = { renameTarget = null },
        )
    }
}

@Composable
private fun FilterTabs(
    uiState: SessionListUiState,
    onFilterChange: (SessionFilter) -> Unit,
    compact: Boolean,
    modifier: Modifier = Modifier,
) {
    val tabs = listOf(
        Triple(SessionFilter.All, "All", uiState.allCount),
        Triple(SessionFilter.Working, "Working", uiState.workingCount),
        Triple(SessionFilter.NeedsInput, "Needs input", uiState.needsInputCount),
    )
    Row(
        modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(vertical = 2.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        tabs.forEach { (filter, label, count) ->
            FilterChip(
                selected = uiState.filter == filter,
                onClick = { onFilterChange(filter) },
                label = { Text("$label $count", fontSize = if (compact) 12.sp else 13.sp) },
            )
        }
    }
}

@Composable
private fun DateHeader(text: String, hPad: androidx.compose.ui.unit.Dp) {
    // Amber uppercase mono, matching the in-chat sidebar section headers (SESSION / MODEL …).
    Text(
        text = text.uppercase(),
        modifier = Modifier
            .fillMaxWidth()
            .background(Surface)
            .padding(start = hPad + 4.dp, end = hPad + 4.dp, top = 12.dp, bottom = 5.dp),
        fontFamily = Opcode42Mono,
        fontSize = 10.5.sp,
        fontWeight = FontWeight.Medium,
        letterSpacing = 0.8.sp,
        color = Secondary,
    )
}

/** Rename a session — prefilled with the current title; Save commits the trimmed text. */
@Composable
private fun RenameSessionDialog(
    current: String?,
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var text by remember { mutableStateOf(current.orEmpty()) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Rename session") },
        text = {
            OutlinedTextField(
                value = text,
                onValueChange = { text = it },
                singleLine = true,
                label = { Text("Title") },
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = {
                    if (text.isNotBlank()) onConfirm(text)
                }),
            )
        },
        confirmButton = {
            TextButton(onClick = { onConfirm(text) }, enabled = text.isNotBlank()) { Text("Save") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}
