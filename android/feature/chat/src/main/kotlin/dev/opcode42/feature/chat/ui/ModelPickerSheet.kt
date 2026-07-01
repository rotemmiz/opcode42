package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.model.AgentInfo
import dev.opcode42.core.model.ModelRef
import dev.opcode42.core.model.ProviderInfo

/** Which sheet the composer's model/agent affordances open. */
enum class PickerTarget { MODEL, AGENT }

/**
 * Model picker (the `/models` command and the status-strip model tap). Models are
 * grouped by provider under purple section headers; a filter field narrows the list.
 * The current model is the amber focal row. The selection threads into the next
 * `POST /session/{id}/message`.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ModelPickerSheet(
    providers: List<ProviderInfo>,
    selectedModel: ModelRef?,
    onSelectModel: (ModelRef) -> Unit,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    var filter by remember { mutableStateOf("") }
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
            SheetTitle("Model")
            FilterField(
                value = filter,
                onValueChange = { filter = it },
                placeholder = "Filter models…",
            )

            // Providers keeping at least one model that matches the filter.
            val visibleProviders = providers.mapNotNull { provider ->
                val models = provider.models.values
                    .filter { it.label.contains(filter, ignoreCase = true) }
                    .sortedBy { it.label.lowercase() }
                if (models.isEmpty()) null else provider to models
            }

            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 460.dp)
                    .padding(horizontal = 8.dp),
            ) {
                for ((provider, models) in visibleProviders) {
                    item(key = "provider:${provider.id}") { SectionHeader(provider.label.uppercase()) }
                    items(models, key = { "model:${provider.id}:${it.id}" }) { model ->
                        val ref = ModelRef(providerID = provider.id, modelID = model.id)
                        ModelRow(
                            name = model.label,
                            descriptor = formatContextWindow(model.limit.context),
                            selected = ref == selectedModel,
                            onClick = { onSelectModel(ref) },
                        )
                    }
                }
                if (visibleProviders.isEmpty()) {
                    item {
                        Text(
                            text = if (providers.all { it.models.isEmpty() }) {
                                "No models reported by this daemon."
                            } else {
                                "No models match “$filter”."
                            },
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

/**
 * Agent-mode picker (the `/agents` command and the status-strip mode-chip tap).
 * Each mode carries a colored marker + a one-line description; the current mode is
 * the amber focal row.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AgentPickerSheet(
    agents: List<AgentInfo>,
    selectedAgent: String?,
    onSelectAgent: (String) -> Unit,
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
            SheetTitle("Agent mode")
            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 460.dp)
                    .padding(horizontal = 8.dp),
            ) {
                items(agents, key = { "agent:${it.name}" }) { agent ->
                    AgentRow(
                        name = agent.name.replaceFirstChar { it.uppercase() },
                        description = agent.description,
                        dotColor = agentDotColor(agent.name),
                        selected = agent.name == selectedAgent,
                        onClick = { onSelectAgent(agent.name) },
                    )
                }
                if (agents.isEmpty()) {
                    item {
                        Text(
                            text = "No agent modes reported by this daemon.",
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
private fun SheetTitle(title: String) {
    Text(
        text = title,
        fontSize = 15.sp,
        fontWeight = FontWeight.SemiBold,
        color = OnSurface,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 10.dp),
    )
}

@Composable
private fun FilterField(value: String, onValueChange: (String) -> Unit, placeholder: String) {
    val shape = RoundedCornerShape(10.dp)
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 16.dp, end = 16.dp, bottom = 8.dp)
            .clip(shape)
            .background(SurfaceContainer)
            .border(1.dp, Hairline, shape)
            .padding(horizontal = 12.dp, vertical = 10.dp),
    ) {
        Icon(
            Icons.Default.Search,
            contentDescription = null,
            tint = OnSurfaceFaint,
            modifier = Modifier.size(16.dp),
        )
        Spacer(Modifier.width(8.dp))
        BasicTextField(
            value = value,
            onValueChange = onValueChange,
            singleLine = true,
            textStyle = TextStyle(
                color = OnSurface,
                fontFamily = Opcode42Mono,
                fontSize = 13.sp,
            ),
            cursorBrush = SolidColor(Primary),
            modifier = Modifier.fillMaxWidth(),
            decorationBox = { inner ->
                if (value.isEmpty()) {
                    Text(
                        placeholder,
                        color = OnSurfaceGhost,
                        fontFamily = Opcode42Mono,
                        fontSize = 13.sp,
                    )
                }
                inner()
            },
        )
    }
}

@Composable
private fun SectionHeader(label: String) {
    Text(
        text = label,
        fontFamily = Opcode42Mono,
        fontSize = 11.sp,
        fontWeight = FontWeight.Bold,
        letterSpacing = 0.5.sp,
        color = HeaderPurple,
        modifier = Modifier.padding(start = 12.dp, end = 12.dp, top = 14.dp, bottom = 4.dp),
    )
}

@Composable
private fun ModelRow(
    name: String,
    descriptor: String?,
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
        // Fixed check gutter so model names align whether or not a row is selected.
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
        if (descriptor != null) {
            Text(
                text = descriptor,
                fontFamily = Opcode42Mono,
                fontSize = 12.sp,
                color = OnSurfaceFaint,
                maxLines = 1,
            )
        }
    }
}

@Composable
private fun AgentRow(
    name: String,
    description: String?,
    dotColor: Color,
    selected: Boolean,
    onClick: () -> Unit,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(6.dp))
            .clickable(onClick = onClick)
            .focalRow(active = selected)
            .heightIn(min = 52.dp)
            .padding(horizontal = 12.dp, vertical = 8.dp),
    ) {
        Box(
            modifier = Modifier
                .size(9.dp)
                .clip(RoundedCornerShape(2.dp))
                .background(dotColor),
        )
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = name,
                fontSize = 14.sp,
                fontWeight = FontWeight.SemiBold,
                color = OnSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (!description.isNullOrBlank()) {
                Text(
                    text = description,
                    fontSize = 12.5.sp,
                    color = OnSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        Box(modifier = Modifier.size(20.dp), contentAlignment = Alignment.Center) {
            if (selected) {
                Icon(
                    Icons.Default.Check,
                    contentDescription = "Selected",
                    tint = Secondary,
                    modifier = Modifier.size(18.dp),
                )
            }
        }
    }
}

/**
 * A stable accent color for an agent-mode marker. The four canonical opencode modes
 * map to fixed semantic colors; any other mode gets a deterministic palette color so
 * it stays consistent across recompositions without inventing per-agent metadata.
 */
@Composable
private fun agentDotColor(name: String): Color = when (name.lowercase()) {
    "build" -> Primary
    "plan" -> HeaderPurple
    "ask" -> LinkCyan
    "review" -> Tertiary
    else -> {
        val palette = listOf(Primary, HeaderPurple, LinkCyan, Tertiary, Secondary)
        palette[((name.hashCode() % palette.size) + palette.size) % palette.size]
    }
}

/**
 * A model's context window as a compact descriptor (200000 → "200K", 1_500_000 → "1.5M").
 * Rounds to the nearest unit (K/M) rather than truncating, so a 1.5M-token window doesn't
 * read as "1M". Returns null when the daemon didn't report a limit, so the row simply omits
 * it — we never fabricate a size the daemon didn't send. Distinct from `formatCompactCount`,
 * which always prints a decimal ("200.0K") — model sizes read cleaner as whole units.
 */
private fun formatContextWindow(context: Double): String? {
    val n = context.toLong()
    return when {
        n <= 0 -> null
        n >= 1_000_000 -> {
            val tenths = (n + 50_000) / 100_000 // round to nearest 0.1M
            if (tenths % 10 == 0L) "${tenths / 10}M" else "${tenths / 10}.${tenths % 10}M"
        }
        n >= 1_000 -> "${(n + 500) / 1_000}K" // round to nearest K
        else -> n.toString()
    }
}
