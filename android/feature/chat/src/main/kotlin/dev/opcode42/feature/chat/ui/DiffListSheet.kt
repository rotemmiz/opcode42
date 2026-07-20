package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*
import dev.opcode42.core.design.text.StartEllipsisText

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.InsertDriveFile
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.model.SnapshotFileDiff

/**
 * The `/diff` viewer — a bottom sheet listing the session's changed files. Each row
 * shows the path and +adds/−dels; tapping a row whose [SnapshotFileDiff.patch] is present
 * expands it inline via the chat's [UnifiedDiffView] (pixel-identical to the inline edit
 * card). Rows without a patch (the working-tree `git status` shape, where the daemon
 * sends no patch) stay non-expandable.
 *
 * Source preference: session patches ([diffs]) when non-empty (they carry patches), else
 * the working-tree [changedFiles] (file list only). An empty list shows "No diffs yet."
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DiffListSheet(
    diffs: Map<String, List<SnapshotFileDiff>>,
    changedFiles: List<SnapshotFileDiff>,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val rows = remember(diffs, changedFiles) {
        val sessionDiffs = diffs.values.flatten().filter { (it.file?.isNotBlank() == true) }
        if (sessionDiffs.isNotEmpty()) {
            sessionDiffs.sortedByDescending { it.additions + it.deletions }
        } else {
            changedFiles.sortedByDescending { it.additions + it.deletions }
        }
    }
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = SurfaceContainerHigh,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(bottom = 24.dp),
        ) {
            Text(
                text = "Changes",
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
                color = OnSurface,
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 4.dp),
            )
            Text(
                text = if (rows.isEmpty()) "No diffs yet." else "${rows.size} ${if (rows.size == 1) "file" else "files"}",
                fontFamily = Opcode42Mono,
                fontSize = 11.5.sp,
                color = OnSurfaceFaint,
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 10.dp),
            )
            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 520.dp)
                    .padding(horizontal = 8.dp),
            ) {
                itemsIndexed(rows, key = { i, d -> d.file ?: "diff-$i" }) { _, diff ->
                    DiffRow(diff)
                }
            }
        }
    }
}

@Composable
private fun DiffRow(diff: SnapshotFileDiff) {
    val file = diff.file?.takeIf { it.isNotBlank() } ?: "unknown"
    val hasPatch = !diff.patch.isNullOrBlank()
    var expanded by rememberSaveable(file) { mutableStateOf(false) }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 2.dp),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(6.dp))
                .then(if (hasPatch) Modifier.clickable { expanded = !expanded } else Modifier)
                .heightIn(min = 44.dp)
                .padding(horizontal = 10.dp, vertical = 6.dp),
        ) {
            Icon(
                Icons.AutoMirrored.Filled.InsertDriveFile,
                contentDescription = null,
                tint = OnSurfaceFaint,
                modifier = Modifier.size(14.dp),
            )
            Spacer(Modifier.width(8.dp))
            StartEllipsisText(
                text = file,
                style = TextStyle(fontFamily = Opcode42Mono, fontSize = 13.sp, color = OnSurface),
                modifier = Modifier.weight(1f),
            )
            Spacer(Modifier.width(8.dp))
            Text(
                text = buildAnnotatedString {
                    withStyle(SpanStyle(color = Tertiary)) { append("+${diff.additions}") }
                    append(" ")
                    withStyle(SpanStyle(color = Error)) { append("−${diff.deletions}") }
                },
                fontFamily = Opcode42Mono,
                fontSize = 12.5.sp,
            )
            if (hasPatch) {
                Spacer(Modifier.width(4.dp))
                Icon(
                    Icons.Default.ChevronRight,
                    contentDescription = null,
                    tint = OnSurfaceFaint,
                    modifier = Modifier
                        .size(18.dp)
                        .rotate(if (expanded) 90f else 0f),
                )
            }
        }
        AnimatedVisibility(
            visible = expanded && hasPatch,
            enter = expandVertically() + fadeIn(),
            exit = shrinkVertically() + fadeOut(),
        ) {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 2.dp, start = 4.dp, end = 4.dp, bottom = 6.dp),
            ) {
                UnifiedDiffView(diffs = listOf(diff))
            }
        }
    }
}