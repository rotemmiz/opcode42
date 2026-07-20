package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * Variant picker (the `/variant` command). Lists the per-model variants the
 * opencode provider catalog reports for the current model (`Model.variants`),
 * plus a "Default" entry to clear the selection. The current variant is the
 * amber focal row. The selection threads into the next
 * `POST /session/{id}/message` as `model.variant`.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VariantPickerSheet(
    variants: List<String>,
    selectedVariant: String?,
    modelLabel: String?,
    onSelectVariant: (String?) -> Unit,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
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
                text = "Variant" + (modelLabel?.let { " · $it" } ?: ""),
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
                color = OnSurface,
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 10.dp),
            )
            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 460.dp)
                    .padding(horizontal = 8.dp),
            ) {
                item(key = "variant:default") {
                    VariantRow(
                        name = "Default",
                        selected = selectedVariant == null,
                        onClick = { onSelectVariant(null) },
                    )
                }
                items(variants, key = { "variant:$it" }) { id ->
                    VariantRow(
                        name = id,
                        selected = id == selectedVariant,
                        onClick = { onSelectVariant(id) },
                    )
                }
            }
        }
    }
}

@Composable
private fun VariantRow(
    name: String,
    selected: Boolean,
    onClick: () -> Unit,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(6.dp))
            .clickable(onClick = onClick)
            .focalRow(active = selected)
            .heightIn(min = 48.dp)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    ) {
        Box(modifier = Modifier.width(22.dp), contentAlignment = Alignment.CenterStart) {
            if (selected) {
                Icon(
                    Icons.Default.Check,
                    contentDescription = "Selected",
                    tint = Secondary,
                    modifier = Modifier.size(16.dp),
                )
            }
        }
        Text(
            text = name,
            fontFamily = Opcode42Mono,
            fontSize = 13.5.sp,
            fontWeight = if (selected) FontWeight.Bold else FontWeight.Normal,
            color = OnSurface,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
    }
}
