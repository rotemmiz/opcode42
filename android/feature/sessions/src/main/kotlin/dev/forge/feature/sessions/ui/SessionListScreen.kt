package dev.forge.feature.sessions.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.ImeAction
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

    var renameTarget by remember { mutableStateOf<Session?>(null) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(if (uiState.showArchived) "Archived" else "Forge") },
                navigationIcon = {
                    if (uiState.showArchived) {
                        IconButton(onClick = { viewModel.toggleShowArchived() }) {
                            Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back to active sessions")
                        }
                    }
                },
                actions = {
                    if (!uiState.showArchived && uiState.archivedCount > 0) {
                        IconButton(onClick = { viewModel.toggleShowArchived() }) {
                            BadgedBox(
                                badge = { Badge { Text("${uiState.archivedCount}") } },
                            ) {
                                Icon(Icons.Default.Archive, contentDescription = "Archived sessions")
                            }
                        }
                    }
                    IconButton(onClick = onSettingsClick) {
                        Icon(Icons.Default.Settings, contentDescription = "Settings")
                    }
                },
            )
        },
        floatingActionButton = {
            if (!uiState.showArchived) {
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
            }
        },
    ) { padding ->
        when {
            uiState.isLoading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator()
            }
            uiState.sessions.isEmpty() && uiState.showArchived -> EmptyArchivedList(
                modifier = Modifier.padding(padding),
            )
            uiState.sessions.isEmpty() -> EmptySessionList(
                onAddServer = onAddServerClick,
                onNewSession = { viewModel.createSession { onSessionClick(it) } },
            )
            else -> SessionList(
                sessions = uiState.sessions,
                showArchived = uiState.showArchived,
                onSessionClick = onSessionClick,
                onRenameSession = { session -> renameTarget = session },
                onArchiveSession = { sessionId -> viewModel.archiveSession(sessionId) },
                onForkSession = { sessionId -> viewModel.forkSession(sessionId) { newSession -> onSessionClick(newSession) } },
                onDeleteSession = { sessionId -> viewModel.deleteSession(sessionId) },
                modifier = Modifier.padding(padding),
            )
        }
    }

    renameTarget?.let { session ->
        RenameSessionDialog(
            current = session.title,
            onConfirm = { title ->
                viewModel.renameSession(session.id, title)
                renameTarget = null
            },
            onDismiss = { renameTarget = null },
        )
    }
}

@Composable
private fun SessionList(
    sessions: List<Session>,
    showArchived: Boolean,
    onSessionClick: (Session) -> Unit,
    onRenameSession: (Session) -> Unit,
    onArchiveSession: (String) -> Unit,
    onForkSession: (String) -> Unit,
    onDeleteSession: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyColumn(modifier = modifier.fillMaxSize()) {
        items(sessions, key = { it.id }) { session ->
            SessionRow(
                session = session,
                showArchived = showArchived,
                onClick = { onSessionClick(session) },
                onRename = { onRenameSession(session) },
                onArchive = { onArchiveSession(session.id) },
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
    showArchived: Boolean,
    onClick: () -> Unit,
    onRename: () -> Unit,
    onArchive: () -> Unit,
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
                text = { Text("Rename session") },
                leadingIcon = { Icon(Icons.Default.Edit, contentDescription = null) },
                onClick = {
                    showMenu = false
                    onRename()
                },
            )
            DropdownMenuItem(
                text = { Text("Fork session") },
                leadingIcon = { Icon(Icons.Default.CallSplit, contentDescription = null) },
                onClick = {
                    showMenu = false
                    onFork()
                },
            )
            // opencode has no un-archive path, so archive is offered only on active rows.
            if (!showArchived) {
                DropdownMenuItem(
                    text = { Text("Archive session") },
                    leadingIcon = { Icon(Icons.Default.Archive, contentDescription = null) },
                    onClick = {
                        showMenu = false
                        onArchive()
                    },
                )
            }
            DropdownMenuItem(
                text = { Text("Delete session") },
                leadingIcon = { Icon(Icons.Default.Delete, contentDescription = null) },
                onClick = {
                    showMenu = false
                    onDelete()
                },
            )
        }
    }
}

/** Rename a session — prefilled with the current title; Save commits the trimmed text. */
@Composable
private fun RenameSessionDialog(
    current: String?,
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var text by remember { mutableStateOf(current.orEmpty()) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Rename session") },
        text = {
            OutlinedTextField(
                value = text,
                onValueChange = { text = it },
                singleLine = true,
                label = { Text("Title") },
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = {
                    if (text.isNotBlank()) onConfirm(text)
                }),
            )
        },
        confirmButton = {
            TextButton(
                onClick = { onConfirm(text) },
                enabled = text.isNotBlank(),
            ) { Text("Save") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
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

@Composable
private fun EmptyArchivedList(modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxSize().padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(
            Icons.Default.Archive,
            contentDescription = null,
            modifier = Modifier.size(64.dp),
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(16.dp))
        Text("No archived sessions", style = MaterialTheme.typography.titleMedium)
    }
}
