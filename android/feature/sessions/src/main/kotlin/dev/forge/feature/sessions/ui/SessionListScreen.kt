package dev.forge.feature.sessions.ui

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
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
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
                FloatingActionButton(
                    onClick = { if (!isCreating) viewModel.createSession { onSessionClick(it) } },
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
            uiState.showArchived && uiState.archivedCount == 0 -> EmptyArchivedList(Modifier.padding(padding))
            !uiState.showArchived && uiState.allCount == 0 -> EmptySessionList(
                onAddServer = onAddServerClick,
                onNewSession = { viewModel.createSession { onSessionClick(it) } },
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
