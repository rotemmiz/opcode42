package dev.opcode42.feature.sessions.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.design.theme.Hairline
import dev.opcode42.core.design.theme.HeaderPurple
import dev.opcode42.core.design.theme.OnSurface
import dev.opcode42.core.design.theme.OnSurfaceFaint
import dev.opcode42.core.design.theme.OnSurfaceGhost
import dev.opcode42.core.design.theme.OnSurfaceVariant
import dev.opcode42.core.design.theme.Primary
import dev.opcode42.core.design.theme.Surface
import dev.opcode42.core.design.theme.SurfaceContainer
import dev.opcode42.core.model.Session
import dev.opcode42.feature.sessions.SessionFilter
import dev.opcode42.feature.sessions.SessionListUiState

/**
 * Self-contained sessions browser shared by the full-screen list and the in-chat left rail:
 * a search field and a recency-ordered `LazyColumn` of [SessionRow]s. The full list keeps
 * status filter tabs (All / Working / Needs input) and date-group headers; the [compact] rail
 * drops the tabs and lists everything flat under a single purple `SESSIONS` header (the date
 * moves into each row's meta line), per the design. Owns the rename dialog so any surface gets
 * rename for free. [containerColor] backs the sticky headers so rows scroll cleanly under them.
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
    containerColor: Color = Surface,
    // Rail collapse animation: 1f = open, 0f = collapsed. Read only in draw/layout lambdas so the
    // per-frame float never recomposes. [onExpand] re-opens the rail when the collapsed search is tapped.
    progress: () -> Float = { 1f },
    onExpand: () -> Unit = {},
) {
    var renameTarget by remember { mutableStateOf<Session?>(null) }
    val hPad = if (compact) 8.dp else 12.dp

    // One row renderer, reused by the flat (rail) and date-grouped (full list) layouts below.
    val rowContent: @Composable (Session) -> Unit = { session ->
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
            progress = progress,
        )
    }

    Column(modifier.fillMaxSize()) {
        SessionSearchField(
            query = uiState.query,
            onQueryChange = onQueryChange,
            compact = compact,
            progress = progress,
            onExpand = onExpand,
            modifier = Modifier.padding(horizontal = hPad, vertical = 6.dp),
        )

        // Filter tabs are a full-list affordance; the rail lists sessions flat (design).
        if (!compact && !uiState.showArchived) {
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
                if (compact) {
                    // Rail: a single purple SESSIONS header over a flat, recency-ordered list.
                    stickyHeader(key = "h:SESSIONS") { SectionHeader("SESSIONS", hPad, containerColor, progress) }
                    items(uiState.groups.flatMap { it.sessions }, key = { it.id }) { rowContent(it) }
                } else {
                    uiState.groups.forEach { group ->
                        stickyHeader(key = "h:${group.header}") {
                            SectionHeader(group.header, hPad, containerColor)
                        }
                        items(group.sessions, key = { it.id }) { rowContent(it) }
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

/**
 * The design's compact boxed search — a single-line field (surface-container fill, hairline
 * border) rather than a tall M3 outlined field, so it stays 38/44dp and the placeholder never
 * wraps in the narrow rail.
 *
 * In the rail it **stays put** and retracts with [progress]: the bordered box narrows with the
 * rail (it's `fillMaxWidth`) to a ~38dp search "dot", the leading search icon stays fully visible,
 * and the placeholder/value/clear fade out. While collapsed, tapping it re-opens the rail ([onExpand]).
 */
@Composable
private fun SessionSearchField(
    query: String,
    onQueryChange: (String) -> Unit,
    compact: Boolean,
    modifier: Modifier = Modifier,
    progress: () -> Float = { 1f },
    onExpand: () -> Unit = {},
) {
    val shape = RoundedCornerShape(if (compact) 8.dp else 14.dp)
    val textSize = if (compact) 13.sp else 14.sp
    // Below the midpoint the field is a non-interactive search dot whose tap re-opens the rail.
    val open by remember { derivedStateOf { progress() > 0.5f } }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = modifier
            .fillMaxWidth()
            .height(if (compact) 38.dp else 44.dp)
            .clip(shape)
            .background(SurfaceContainer)
            .border(1.dp, Hairline, shape)
            .then(if (!open) Modifier.clickable(onClick = onExpand) else Modifier)
            // Compact: start inset 14 puts the icon at rail-x 22 → centered in the 60dp band, so it
            // sits dead-center when collapsed and stays put (no glide) as the rail retracts.
            .padding(start = if (compact) 14.dp else 11.dp, end = 11.dp),
    ) {
        Icon(
            Icons.Default.Search,
            contentDescription = null,
            tint = OnSurfaceFaint,
            modifier = Modifier.size(if (compact) 16.dp else 18.dp),
        )
        Spacer(Modifier.width(9.dp))
        Box(
            Modifier.weight(1f).graphicsLayer { alpha = progress() },
            contentAlignment = Alignment.CenterStart,
        ) {
            if (query.isEmpty()) {
                Text("Search sessions…", color = OnSurfaceGhost, fontSize = textSize, maxLines = 1)
            }
            // The input is inert when collapsed (taps go to the box's onExpand instead).
            if (open) {
                BasicTextField(
                    value = query,
                    onValueChange = onQueryChange,
                    singleLine = true,
                    textStyle = TextStyle(color = OnSurface, fontSize = textSize),
                    cursorBrush = SolidColor(Primary),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
        if (query.isNotEmpty() && open) {
            Icon(
                Icons.Default.Close,
                contentDescription = "Clear search",
                tint = OnSurfaceFaint,
                modifier = Modifier
                    .size(18.dp)
                    .graphicsLayer { alpha = progress() }
                    .clickable { onQueryChange("") },
            )
        }
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
private fun SectionHeader(
    text: String,
    hPad: androidx.compose.ui.unit.Dp,
    bg: Color,
    progress: () -> Float = { 1f },
) {
    // Purple uppercase sans — the design's one section-header voice (rail SESSIONS, date
    // groups, the right info panel, the phone list label all share it). [bg] matches the
    // surface behind it so rows scroll cleanly under the sticky header. In the rail it fades
    // out (height kept) as the rail collapses, so rows below keep their Y.
    Text(
        text = text.uppercase(),
        modifier = Modifier
            .fillMaxWidth()
            .graphicsLayer { alpha = progress() }
            .background(bg)
            .padding(start = hPad + 4.dp, end = hPad + 4.dp, top = 12.dp, bottom = 6.dp),
        fontSize = 11.sp,
        fontWeight = FontWeight.Bold,
        letterSpacing = 0.6.sp,
        color = HeaderPurple,
        // Keep a single line so the header's height never grows (and pushes the rows down) as the
        // rail narrows past the label's width — it just clips while fading out.
        maxLines = 1,
        softWrap = false,
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
