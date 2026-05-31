package dev.forge.feature.sessions.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.forge.core.model.Session
import dev.forge.feature.sessions.SessionListViewModel

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SessionListScreen(
    onSessionClick: (Session) -> Unit,
    onAddServerClick: () -> Unit,
    onSettingsClick: () -> Unit,
    viewModel: SessionListViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    val isCreating by viewModel.isCreating.collectAsStateWithLifecycle()

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Forge") },
                actions = {
                    IconButton(onClick = onSettingsClick) {
                        Icon(Icons.Default.Settings, contentDescription = "Settings")
                    }
                },
            )
        },
        floatingActionButton = {
            FloatingActionButton(
                onClick = {
                    if (!isCreating) viewModel.createSession { onSessionClick(it) }
                },
            ) {
                if (isCreating) {
                    CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
                } else {
                    Icon(Icons.Default.Add, contentDescription = "New session")
                }
            }
        },
    ) { padding ->
        when {
            uiState.isLoading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator()
            }
            uiState.sessions.isEmpty() -> EmptySessionList(
                onAddServer = onAddServerClick,
                onNewSession = { viewModel.createSession { onSessionClick(it) } },
            )
            else -> SessionList(
                sessions = uiState.sessions,
                onSessionClick = onSessionClick,
                onForkSession = { sessionId -> viewModel.forkSession(sessionId) { newSession -> onSessionClick(newSession) } },
                onDeleteSession = { sessionId -> viewModel.deleteSession(sessionId) },
                modifier = Modifier.padding(padding),
            )
        }
    }
}

@Composable
private fun SessionList(
    sessions: List<Session>,
    onSessionClick: (Session) -> Unit,
    onForkSession: (String) -> Unit,
    onDeleteSession: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyColumn(modifier = modifier.fillMaxSize()) {
        items(sessions, key = { it.id }) { session ->
            SessionRow(
                session = session,
                onClick = { onSessionClick(session) },
                onFork = { onForkSession(session.id) },
                onDelete = { onDeleteSession(session.id) },
            )
            HorizontalDivider()
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun SessionRow(
    session: Session,
    onClick: () -> Unit,
    onFork: () -> Unit,
    onDelete: () -> Unit,
) {
    var showMenu by remember { mutableStateOf(false) }

    Box(modifier = Modifier.fillMaxWidth()) {
        ListItem(
            headlineContent = {
                Text(
                    text = session.title ?: session.id,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            },
            supportingContent = session.directory?.let { dir ->
                { Text(dir, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            },
            trailingContent = {
                session.tokens?.let { tokens ->
                    Text(
                        text = "${(tokens.input + tokens.output).toLong() / 1000}K tokens",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            },
            modifier = Modifier.combinedClickable(
                onClick = onClick,
                onLongClick = { showMenu = true },
            ),
        )
        DropdownMenu(
            expanded = showMenu,
            onDismissRequest = { showMenu = false },
        ) {
            DropdownMenuItem(
                text = { Text("Fork session") },
                onClick = {
                    showMenu = false
                    onFork()
                },
            )
            DropdownMenuItem(
                text = { Text("Delete session") },
                onClick = {
                    showMenu = false
                    onDelete()
                },
            )
        }
    }
}

@Composable
private fun EmptySessionList(onAddServer: () -> Unit, onNewSession: () -> Unit) {
    Column(
        modifier = Modifier.fillMaxSize().padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(
            Icons.Default.Cloud,
            contentDescription = null,
            modifier = Modifier.size(64.dp),
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(16.dp))
        Text("No sessions yet", style = MaterialTheme.typography.titleMedium)
        Spacer(Modifier.height(8.dp))
        Text(
            "Add a server to connect, then start a new session.",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(24.dp))
        OutlinedButton(onClick = onAddServer) { Text("Add Server") }
        Spacer(Modifier.height(8.dp))
        Button(onClick = onNewSession) { Text("New Session") }
    }
}
