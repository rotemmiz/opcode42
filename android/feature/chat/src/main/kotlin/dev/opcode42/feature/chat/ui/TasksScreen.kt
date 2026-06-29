package dev.opcode42.feature.chat.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.opcode42.feature.chat.ChatViewModel
import dev.opcode42.feature.chat.TodoItem

/**
 * Tasks board for a session — the todos list with status chips (design:
 * "tasks board · list + status chips"). Reached from the chat todos sheet.
 * Per-session, so it's a pushed screen rather than a global bottom-nav tab.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TasksScreen(
    onNavigateBack: () -> Unit,
    viewModel: ChatViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    val todos = uiState.todos
    val active = todos.count { it.status == "in_progress" || it.status == "pending" }
    val done = todos.count { it.status == "completed" }

    Scaffold(
        containerColor = Surface,
        topBar = {
            Column {
                TopAppBar(
                    title = {
                        Column {
                            Text("Tasks", style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.Medium), color = OnSurface)
                            if (todos.isNotEmpty()) {
                                Text(
                                    text = "$active active · $done done",
                                    fontFamily = Opcode42Mono,
                                    fontSize = 11.5.sp,
                                    color = OnSurfaceFaint,
                                )
                            }
                        }
                    },
                    navigationIcon = {
                        IconButton(onClick = onNavigateBack) {
                            Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back", tint = OnSurface)
                        }
                    },
                    colors = TopAppBarDefaults.topAppBarColors(containerColor = Surface, titleContentColor = OnSurface),
                )
                HorizontalDivider(color = Hairline)
            }
        },
    ) { padding ->
        if (todos.isEmpty()) {
            Box(Modifier.fillMaxSize().padding(padding), contentAlignment = Alignment.Center) {
                Text("No todos yet", color = OnSurfaceFaint, fontSize = 14.sp)
            }
        } else {
            LazyColumn(modifier = Modifier.fillMaxSize().padding(padding)) {
                items(todos) { todo ->
                    HorizontalDivider(color = Hairline)
                    TaskRow(todo)
                }
            }
        }
    }
}

@Composable
private fun TaskRow(todo: TodoItem) {
    val doing = todo.status == "in_progress"
    val done = todo.status == "completed"
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 52.dp)
            .padding(horizontal = 16.dp, vertical = 8.dp),
    ) {
        TodoStatusGlyph(doing = doing, done = done)
        Text(
            text = todo.content,
            fontSize = 14.5.sp,
            lineHeight = 20.sp,
            fontWeight = if (doing) FontWeight.SemiBold else FontWeight.Normal,
            color = when {
                doing -> Secondary
                done -> OnSurfaceVariant
                else -> OnSurface
            },
            modifier = Modifier.weight(1f),
        )
        StatusBadge(todo.status)
    }
}

/** opcode42-bits Badge: a colored dot + label, color-coded by status. */
@Composable
private fun StatusBadge(status: String) {
    val (color, label) = when (status) {
        "in_progress" -> Secondary to "in progress"
        "completed" -> Tertiary to "done"
        "cancelled" -> Error to "cancelled"
        else -> OnSurfaceVariant to "todo"
    }
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(5.dp)) {
        Box(Modifier.size(7.dp).clip(RoundedCornerShape(4.dp)).background(color))
        Text(label, fontSize = 12.sp, color = color)
    }
}
