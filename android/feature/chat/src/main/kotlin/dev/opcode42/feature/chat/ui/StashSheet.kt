package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*
import dev.opcode42.feature.chat.data.StashStore

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch

/**
 * Stashed prompt drafts (the `/stash` command). Drafts persist locally via
 * [StashStore] (no daemon endpoint). Each row can be loaded back into the
 * composer ("Load") or removed ("Delete"). An "Add current" affordance in the
 * header stashes the composer's current text — the entry point lives in the
 * sheet itself rather than on a composer affordance, keeping the stash surface
 * self-contained.
 *
 * Loading a draft calls back to [onLoad], which threads the draft into the
 * composer's external-draft channel (see `PromptInput`'s `externalDraft`
 * parameter). The sheet dismisses itself on load.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun StashSheet(
    store: StashStore,
    composerDraftProvider: () -> String,
    onLoad: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val scope = rememberCoroutineScope()
    val drafts = remember { mutableStateListOf<String>() }
    var loaded by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) { drafts.addAll(store.list()) }

    fun addCurrent() {
        val draft = composerDraftProvider()
        if (draft.isBlank()) return
        scope.launch {
            store.add(draft)
            drafts.clear()
            drafts.addAll(store.list())
        }
    }

    fun deleteAt(index: Int) {
        if (index !in drafts.indices) return
        scope.launch {
            store.delete(index)
            drafts.clear()
            drafts.addAll(store.list())
        }
    }

    fun loadAt(index: Int) {
        if (index !in drafts.indices) return
        val draft = drafts[index]
        if (!loaded) {
            loaded = true
            onLoad(draft)
        }
        scope.launch { sheetState.hide() }.invokeOnCompletion { onDismiss() }
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
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 16.dp, end = 8.dp, top = 4.dp, bottom = 10.dp),
            ) {
                Text(
                    text = "Stashed drafts",
                    fontSize = 15.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = OnSurface,
                    modifier = Modifier.weight(1f),
                )
                AddCurrentButton(onClick = { addCurrent() })
            }

            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 460.dp)
                    .padding(horizontal = 8.dp),
            ) {
                itemsIndexed(drafts, key = { i, _ -> "stash:$i" }) { index, draft ->
                    StashRow(
                        draft = draft,
                        onLoad = { loadAt(index) },
                        onDelete = { deleteAt(index) },
                    )
                }
                if (drafts.isEmpty()) {
                    item {
                        Text(
                            text = "No stashed drafts yet. Type a prompt and tap “+” to stash it.",
                            fontSize = 13.sp,
                            color = OnSurfaceVariant,
                            modifier = Modifier.padding(horizontal = 8.dp, vertical = 12.dp),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun AddCurrentButton(onClick: () -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
        modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    ) {
        Icon(
            Icons.Default.Add,
            contentDescription = "Add current draft",
            tint = OnPrimary,
            modifier = Modifier.size(16.dp),
        )
        Text(
            text = "Add current",
            fontSize = 13.sp,
            fontWeight = FontWeight.Medium,
            color = OnPrimary,
        )
    }
}

@Composable
private fun StashRow(
    draft: String,
    onLoad: () -> Unit,
    onDelete: () -> Unit,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(6.dp))
            .heightIn(min = 48.dp)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    ) {
        Text(
            text = draft,
            fontFamily = Opcode42Mono,
            fontSize = 13.sp,
            color = OnSurface,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        Spacer(Modifier.width(4.dp))
        Text(
            text = "Load",
            fontSize = 13.sp,
            fontWeight = FontWeight.Medium,
            color = LinkCyan,
            modifier = Modifier
                .clip(RoundedCornerShape(6.dp))
                .clickable(onClick = onLoad)
                .padding(horizontal = 10.dp, vertical = 6.dp),
        )
        Text(
            text = "Delete",
            fontSize = 13.sp,
            color = OnSurfaceFaint,
            modifier = Modifier
                .clip(RoundedCornerShape(6.dp))
                .clickable(onClick = onDelete)
                .padding(horizontal = 8.dp, vertical = 6.dp),
        )
    }
}
