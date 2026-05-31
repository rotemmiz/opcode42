package dev.forge.feature.chat.ui

import androidx.compose.animation.core.Animatable
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Checklist
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.forge.feature.chat.TodoItem
import kotlinx.coroutines.launch

private val PeekHeight = 50.dp
private val ExpandedHeight = 308.dp
private val ScrimColor = Color(0x80080909) // rgba(8,9,10,0.5)

/**
 * Draggable todos sheet (design §3). Docks at a 50dp peek above the status
 * strip; drag or tap the handle to expand to ~308dp. When expanded a scrim
 * covers the stream (tap to collapse). Fills its parent Box and anchors to the
 * bottom, so place it as the last child of the stream's Box.
 */
@Composable
fun TodoSheet(
    todos: List<TodoItem>,
    onOpenTasksBoard: () -> Unit,
    modifier: Modifier = Modifier,
) {
    if (todos.isEmpty()) return

    val density = LocalDensity.current
    val peekPx = with(density) { PeekHeight.toPx() }
    val expandedPx = with(density) { ExpandedHeight.toPx() }
    val midPx = (peekPx + expandedPx) / 2

    val height = remember { Animatable(peekPx) }
    val scope = rememberCoroutineScope()
    val open by remember { derivedStateOf { height.value > peekPx + with(density) { 24.dp.toPx() } } }

    fun snapTo(target: Float) = scope.launch { height.animateTo(target) }

    Box(modifier = modifier.fillMaxSize()) {
        if (open) {
            Box(
                Modifier
                    .fillMaxSize()
                    .background(ScrimColor)
                    .clickable(
                        indication = null,
                        interactionSource = remember { androidx.compose.foundation.interaction.MutableInteractionSource() },
                    ) { snapTo(peekPx) },
            )
        }

        Column(
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .fillMaxWidth()
                .height(with(density) { height.value.toDp() })
                .clip(RoundedCornerShape(topStart = 16.dp, topEnd = 16.dp))
                .background(SurfaceContainerHigh),
        ) {
            HandleAndPeekRow(
                todos = todos,
                open = open,
                onToggle = { snapTo(if (open) peekPx else expandedPx) },
                onDrag = { dy ->
                    scope.launch { height.snapTo((height.value - dy).coerceIn(peekPx, expandedPx)) }
                },
                onDragEnd = { snapTo(if (height.value > midPx) expandedPx else peekPx) },
            )
            TodoList(todos = todos, onOpenTasksBoard = onOpenTasksBoard)
        }
    }
}

@Composable
private fun HandleAndPeekRow(
    todos: List<TodoItem>,
    open: Boolean,
    onToggle: () -> Unit,
    onDrag: (Float) -> Unit,
    onDragEnd: () -> Unit,
) {
    val activeCount = todos.count { it.status == "in_progress" || it.status == "pending" }
    val doneCount = todos.count { it.status == "completed" }

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .pointerInput(Unit) {
                detectVerticalDragGestures(
                    onVerticalDrag = { _, dy -> onDrag(dy) },
                    onDragEnd = { onDragEnd() },
                )
            }
            .pointerInput(Unit) { detectTapGestures { onToggle() } }
            .padding(horizontal = 14.dp, vertical = 8.dp),
    ) {
        // drag handle
        Box(
            Modifier
                .align(Alignment.CenterHorizontally)
                .padding(bottom = 9.dp)
                .size(width = 32.dp, height = 4.dp)
                .clip(RoundedCornerShape(2.dp))
                .background(OnSurfaceFaint),
        )
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Icon(Icons.Default.Checklist, contentDescription = null, tint = HeaderPurple, modifier = Modifier.size(16.dp))
            Text("Todos", fontSize = 14.sp, fontWeight = FontWeight.Medium, color = OnSurface)
            Text(
                text = "tasks.md",
                fontFamily = FontFamily.Monospace,
                fontSize = 11.5.sp,
                color = LinkCyan,
                modifier = Modifier
                    .clip(RoundedCornerShape(4.dp))
                    .background(LinkCyan.copy(alpha = 0.12f))
                    .padding(horizontal = 7.dp, vertical = 1.dp),
            )
            Spacer(Modifier.weight(1f))
            Text(
                text = buildAnnotatedString {
                    withStyle(androidx.compose.ui.text.SpanStyle(color = Secondary, fontWeight = FontWeight.Bold)) {
                        append("$activeCount")
                    }
                    append(" active · $doneCount done")
                },
                fontSize = 12.sp,
                color = OnSurfaceVariant,
            )
            Icon(
                if (open) Icons.Default.ExpandMore else Icons.Default.ExpandLess,
                contentDescription = null,
                tint = OnSurfaceFaint,
                modifier = Modifier.size(16.dp),
            )
        }
    }
}

@Composable
private fun TodoList(todos: List<TodoItem>, onOpenTasksBoard: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(start = 14.dp, end = 14.dp, bottom = 14.dp, top = 2.dp),
    ) {
        todos.forEachIndexed { index, todo ->
            if (index > 0) androidx.compose.material3.HorizontalDivider(color = Hairline)
            TodoRow(todo)
        }
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 12.dp)
                .clickable { onOpenTasksBoard() },
        ) {
            Icon(Icons.Default.Checklist, contentDescription = null, tint = LinkCyan, modifier = Modifier.size(14.dp))
            Text("Open tasks board", fontSize = 13.sp, color = LinkCyan)
            Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = null, tint = LinkCyan, modifier = Modifier.size(14.dp))
        }
    }
}

@Composable
private fun TodoRow(todo: TodoItem) {
    val doing = todo.status == "in_progress"
    val done = todo.status == "completed"
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 46.dp),
    ) {
        TodoStatusGlyph(doing = doing, done = done)
        Text(
            text = todo.content,
            fontSize = 14.sp,
            fontWeight = if (doing) FontWeight.SemiBold else FontWeight.Normal,
            color = when {
                doing -> Secondary
                done -> OnSurfaceVariant
                else -> OnSurface
            },
            modifier = Modifier.weight(1f),
        )
    }
}

@Composable
internal fun TodoStatusGlyph(doing: Boolean, done: Boolean) {
    when {
        done -> Box(
            Modifier.size(20.dp).clip(RoundedCornerShape(10.dp)).background(Tertiary),
            contentAlignment = Alignment.Center,
        ) {
            Icon(Icons.Default.Check, contentDescription = null, tint = OnPrimary, modifier = Modifier.size(13.dp))
        }
        doing -> Box(
            Modifier
                .size(20.dp)
                .clip(RoundedCornerShape(10.dp))
                .background(Secondary.copy(alpha = 0.18f))
                .padding(2.dp),
            contentAlignment = Alignment.Center,
        ) {
            Box(Modifier.size(7.dp).clip(RoundedCornerShape(4.dp)).background(Secondary))
        }
        else -> Box(
            Modifier
                .size(20.dp)
                .border(2.dp, Outline, RoundedCornerShape(5.dp)),
        )
    }
}
