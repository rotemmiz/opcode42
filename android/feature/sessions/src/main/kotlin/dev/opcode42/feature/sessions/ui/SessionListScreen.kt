package dev.opcode42.feature.sessions.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Archive
import androidx.compose.material.icons.filled.Cloud
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.opcode42.core.model.Session
import dev.opcode42.feature.sessions.SessionListEvent
import dev.opcode42.feature.sessions.SessionListViewModel

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SessionListScreen(
    onSessionClick: (Session) -> Unit,
    onNewSession: () -> Unit,
    onAddServerClick: () -> Unit,
    onSettingsClick: () -> Unit,
    viewModel: SessionListViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()

    // Surface one-shot errors (a failed rename/archive/delete/create/reply) as a snackbar.
    val snackbarHostState = remember { SnackbarHostState() }
    LaunchedEffect(Unit) {
        viewModel.events.collect { event ->
            when (event) {
                is SessionListEvent.ShowError -> snackbarHostState.showSnackbar(event.message)
            }
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbarHostState) },
        topBar = {
            TopAppBar(
                title = { Text(if (uiState.showArchived) "Archived" else "Opcode42") },
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
                            BadgedBox(badge = { Badge { Text("${uiState.archivedCount}") } }) {
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
                // Navigates to the lazy draft; the session is created only on first prompt.
                FloatingActionButton(onClick = onNewSession) {
                    Icon(Icons.Default.Add, contentDescription = "New session")
                }
            }
        },
    ) { padding ->
        when {
            // Spinner / error only when there's nothing to show yet — a refresh on reconnect must
            // not flash over an already-populated list.
            uiState.isLoading && uiState.allCount == 0 && uiState.archivedCount == 0 ->
                Box(Modifier.fillMaxSize().padding(padding), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator()
                }
            uiState.error != null && uiState.allCount == 0 && uiState.archivedCount == 0 ->
                SessionListError(
                    message = uiState.error!!,
                    onRetry = viewModel::loadSessions,
                    modifier = Modifier.padding(padding),
                )
            uiState.showArchived && uiState.archivedCount == 0 -> EmptyArchivedList(Modifier.padding(padding))
            !uiState.showArchived && uiState.allCount == 0 -> EmptySessionList(
                onAddServer = onAddServerClick,
                onNewSession = onNewSession,
            )
            else -> SessionBrowser(
                uiState = uiState,
                activeSessionId = null,
                onOpen = onSessionClick,
                onQueryChange = viewModel::setQuery,
                onFilterChange = viewModel::setFilter,
                onRename = viewModel::renameSession,
                onArchive = viewModel::archiveSession,
                onFork = { id -> viewModel.forkSession(id) { onSessionClick(it) } },
                onDelete = viewModel::deleteSession,
                onReplyPermission = viewModel::replyPermission,
                onReplyQuestion = viewModel::replyQuestion,
                onSkipQuestion = viewModel::rejectQuestion,
                modifier = Modifier.padding(padding),
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

@Composable
private fun SessionListError(message: String, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxSize().padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text("Couldn't load sessions", style = MaterialTheme.typography.titleMedium)
        Spacer(Modifier.height(8.dp))
        Text(
            message,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(24.dp))
        Button(onClick = onRetry) { Text("Retry") }
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
