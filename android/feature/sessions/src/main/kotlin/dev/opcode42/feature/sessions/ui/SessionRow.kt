package dev.opcode42.feature.sessions.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.CallSplit
import androidx.compose.material.icons.filled.Archive
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.design.theme.Error
import dev.opcode42.core.design.theme.OnSurface
import dev.opcode42.core.design.theme.OnSurfaceGhost
import dev.opcode42.core.design.theme.OnSurfaceVariant
import dev.opcode42.core.design.theme.Primary
import dev.opcode42.core.design.theme.Secondary
import dev.opcode42.core.design.theme.SecondaryContainer
import dev.opcode42.core.model.PermissionRequest
import dev.opcode42.core.model.QuestionRequest
import dev.opcode42.core.model.Session
import dev.opcode42.feature.sessions.relativeTime

/**
 * One rich session row shared by the full-screen list and the in-chat left rail:
 *   ● Title …                         2m
 *     returnzero_2
 * A leading status dot (spinner while busy), the title, a muted directory-basename +
 * relative-time meta line, a long-press menu (Rename / Fork / Archive / Delete), and the
 * inline permission/question affordances ([SessionPendingActions]) when the session needs
 * the user. [compact] tightens type/padding for the narrow rail.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
internal fun SessionRow(
    session: Session,
    isActive: Boolean,
    status: String?,
    pendingPermission: PermissionRequest?,
    pendingQuestion: QuestionRequest?,
    showArchived: Boolean,
    onClick: () -> Unit,
    onRename: () -> Unit,
    onArchive: () -> Unit,
    onFork: () -> Unit,
    onDelete: () -> Unit,
    onApprove: () -> Unit,
    onDeny: () -> Unit,
    onReply: (String) -> Unit,
    onSkip: () -> Unit,
    modifier: Modifier = Modifier,
    compact: Boolean = false,
) {
    var showMenu by remember { mutableStateOf(false) }
    val needsInput = pendingPermission != null || pendingQuestion != null
    val titleSize = if (compact) 13.sp else 15.sp
    val metaSize = if (compact) 11.sp else 12.sp
    val vPad = if (compact) 7.dp else 10.dp
    val hPad = if (compact) 12.dp else 16.dp
    val accent = Secondary

    Box(
        modifier
            .fillMaxWidth()
            // Active row: amber selection tint + a 2.5dp amber accent rail down the left.
            .then(
                if (isActive) {
                    Modifier
                        .background(SecondaryContainer)
                        .drawBehind { drawRect(accent, size = Size(2.5.dp.toPx(), size.height)) }
                } else {
                    Modifier
                },
            ),
    ) {
        Column(Modifier.fillMaxWidth()) {
            // Only the title row opens the session; the inline actions are siblings outside
            // the clickable so tapping a button doesn't also navigate.
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier
                    .fillMaxWidth()
                    .combinedClickable(onClick = onClick, onLongClick = { showMenu = true })
                    .padding(horizontal = hPad, vertical = vPad),
            ) {
                StatusLeading(busy = isSessionBusy(status), needsInput = needsInput, isActive = isActive)
                Spacer(Modifier.width(10.dp))
                Column(Modifier.weight(1f)) {
                    Text(
                        text = session.title?.takeIf { it.isNotBlank() } ?: "New session",
                        fontSize = titleSize,
                        fontWeight = if (isActive) FontWeight.Medium else FontWeight.Normal,
                        color = OnSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    val dir = session.directory?.substringAfterLast('/')?.takeIf { it.isNotBlank() }
                    val rel = relativeTime(session.time?.updated ?: session.time?.created ?: 0L)
                        .takeIf { it.isNotEmpty() }
                    val meta = listOfNotNull(dir, rel).joinToString("  ·  ")
                    if (meta.isNotEmpty()) {
                        Text(
                            text = meta,
                            fontSize = metaSize,
                            color = OnSurfaceVariant,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
            }
            if (needsInput) {
                SessionPendingActions(
                    permission = pendingPermission,
                    question = pendingQuestion,
                    onApprove = onApprove,
                    onDeny = onDeny,
                    onReply = onReply,
                    onSkip = onSkip,
                    modifier = Modifier.padding(start = hPad, end = hPad, bottom = 10.dp),
                )
            }
        }
        DropdownMenu(expanded = showMenu, onDismissRequest = { showMenu = false }) {
            DropdownMenuItem(
                text = { Text("Rename session") },
                leadingIcon = { Icon(Icons.Default.Edit, contentDescription = null) },
                onClick = { showMenu = false; onRename() },
            )
            DropdownMenuItem(
                text = { Text("Fork session") },
                leadingIcon = { Icon(Icons.AutoMirrored.Filled.CallSplit, contentDescription = null) },
                onClick = { showMenu = false; onFork() },
            )
            // opencode has no un-archive path, so archive is offered only on active rows.
            if (!showArchived) {
                DropdownMenuItem(
                    text = { Text("Archive session") },
                    leadingIcon = { Icon(Icons.Default.Archive, contentDescription = null) },
                    onClick = { showMenu = false; onArchive() },
                )
            }
            DropdownMenuItem(
                text = { Text("Delete session") },
                leadingIcon = { Icon(Icons.Default.Delete, contentDescription = null) },
                onClick = { showMenu = false; onDelete() },
            )
        }
    }
}

/** Leading status indicator: spinner while busy, else a dot (needs-input / active / idle). */
@Composable
private fun StatusLeading(busy: Boolean, needsInput: Boolean, isActive: Boolean) {
    if (busy) {
        SessionStatusSpinner("busy", Modifier)
        return
    }
    val color = when {
        needsInput -> Error
        isActive -> Primary
        else -> OnSurfaceGhost
    }
    Box(Modifier.size(12.dp), contentAlignment = Alignment.Center) {
        Box(Modifier.size(7.dp).clip(CircleShape).background(color))
    }
}
