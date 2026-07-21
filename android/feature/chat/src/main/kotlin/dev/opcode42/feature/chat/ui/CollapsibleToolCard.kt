package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp

/**
 * The shared shell for every collapsible card in the chat stream — Edit, Bash, Write,
 * Subagent, Question. One visual language: hairline `OutlineVariant` border on
 * `SurfaceContainer`, a 46dp header row with a leading glyph + annotated title +
 * optional trailing meta + chevron, and a divided body that only renders when expanded.
 *
 * Slots:
 *  - [leading]   — the header's leading icon (usually 16dp, brand color).
 *  - [title]     — the header title; rendered in a weighted box so [trailing] + the
 *                  chevron pin to the end. Callers typically use `Opcode42Mono` 13sp
 *                  with `SpanStyle` color accents, matching the tool-row grammar.
 *  - [trailing]  — optional meta to the left of the chevron (status glyph, counts, "1 of N").
 *  - [body]      — the expanded content; sits below a `Hairline` divider. Interactive
 *                  bodies (options, buttons, text fields) use standard M3 typography;
 *                  code/output bodies use `Opcode42Typography.code` on `SurfaceContainerLowest`.
 *
 * The chevron is provided by the shell so every card collapses the same way.
 */
@Composable
fun CollapsibleToolCard(
    expanded: Boolean,
    onToggle: () -> Unit,
    modifier: Modifier = Modifier,
    leading: @Composable RowScope.() -> Unit = {},
    title: @Composable RowScope.() -> Unit,
    trailing: @Composable RowScope.() -> Unit = {},
    body: @Composable () -> Unit,
) {
    Column(
        modifier = modifier
            .padding(horizontal = 14.dp, vertical = 4.dp)
            .clip(Opcode42Shapes.sm)
            .background(SurfaceContainer)
            .border(1.dp, OutlineVariant, Opcode42Shapes.sm),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(8.dp),
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 46.dp)
                .clickable { onToggle() }
                .padding(horizontal = 12.dp, vertical = 10.dp),
        ) {
            this.leading()
            title()
            this.trailing()
            Icon(
                if (expanded) Icons.Default.ExpandLess else Icons.Default.ExpandMore,
                contentDescription = null,
                tint = OnSurfaceVariant,
                modifier = Modifier.size(16.dp),
            )
        }
        if (expanded) {
            HorizontalDivider(color = Hairline)
            body()
        }
    }
}