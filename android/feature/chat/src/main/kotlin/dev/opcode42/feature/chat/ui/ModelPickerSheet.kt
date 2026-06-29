package dev.opcode42.feature.chat.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.model.AgentInfo
import dev.opcode42.core.model.ModelRef
import dev.opcode42.core.model.ProviderInfo

/**
 * Agent + model picker (the StatusStrip's tap target). Lets the user choose which agent
 * and which provider/model handle upcoming prompts; the selection threads into the next
 * `POST /session/{id}/message`. Empty sections are hidden so a bare daemon still works.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ModelPickerSheet(
    providers: List<ProviderInfo>,
    agents: List<AgentInfo>,
    selectedModel: ModelRef?,
    selectedAgent: String?,
    onSelectModel: (ModelRef) -> Unit,
    onSelectAgent: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = SurfaceContainerHigh,
    ) {
        LazyColumn(
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(max = 520.dp)
                .padding(start = 8.dp, end = 8.dp, bottom = 24.dp),
        ) {
            if (agents.isNotEmpty()) {
                item { SectionHeader("AGENT") }
                items(agents, key = { "agent:${it.name}" }) { agent ->
                    PickerRow(
                        title = agent.name.replaceFirstChar { it.uppercase() },
                        subtitle = agent.description,
                        selected = agent.name == selectedAgent,
                        onClick = { onSelectAgent(agent.name) },
                    )
                }
            }

            for (provider in providers) {
                val models = provider.models.values.sortedBy { it.label.lowercase() }
                if (models.isEmpty()) continue
                item(key = "provider:${provider.id}") { SectionHeader(provider.label.uppercase()) }
                items(models, key = { "model:${provider.id}:${it.id}" }) { model ->
                    val ref = ModelRef(providerID = provider.id, modelID = model.id)
                    PickerRow(
                        title = model.label,
                        subtitle = null,
                        selected = ref == selectedModel,
                        onClick = { onSelectModel(ref) },
                    )
                }
            }

            if (agents.isEmpty() && providers.all { it.models.isEmpty() }) {
                item {
                    Text(
                        text = "No agents or models reported by this daemon.",
                        fontSize = 13.sp,
                        color = OnSurfaceVariant,
                        modifier = Modifier.padding(horizontal = 12.dp, vertical = 12.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun SectionHeader(label: String) {
    Text(
        text = label,
        fontFamily = Opcode42Mono,
        fontSize = 11.sp,
        fontWeight = FontWeight.Bold,
        color = OnSurfaceFaint,
        modifier = Modifier.padding(start = 12.dp, end = 12.dp, top = 14.dp, bottom = 4.dp),
    )
}

@Composable
private fun PickerRow(
    title: String,
    subtitle: String?,
    selected: Boolean,
    onClick: () -> Unit,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .heightIn(min = 48.dp)
            .padding(horizontal = 12.dp, vertical = 6.dp),
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = title,
                fontFamily = Opcode42Mono,
                fontSize = 13.5.sp,
                color = if (selected) Primary else OnSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (!subtitle.isNullOrBlank()) {
                Text(
                    text = subtitle,
                    fontSize = 12.sp,
                    color = OnSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        Box(modifier = Modifier.size(20.dp), contentAlignment = Alignment.Center) {
            if (selected) {
                Icon(
                    Icons.Default.Check,
                    contentDescription = "Selected",
                    tint = Primary,
                    modifier = Modifier.size(18.dp),
                )
            }
        }
    }
}
