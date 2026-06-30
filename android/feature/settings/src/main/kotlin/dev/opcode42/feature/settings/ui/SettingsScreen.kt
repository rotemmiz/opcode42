package dev.opcode42.feature.settings.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.opcode42.feature.connections.ServerConnection
import dev.opcode42.feature.settings.SettingsViewModel

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    onNavigateBack: () -> Unit,
    onAddServer: () -> Unit,
    viewModel: SettingsViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Settings") },
                navigationIcon = {
                    IconButton(onClick = onNavigateBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                    }
                },
            )
        }
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            // ── Servers ──────────────────────────────────────────────────────
            ListItem(
                headlineContent = { Text("Servers", style = MaterialTheme.typography.labelLarge) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
            )

            state.connections.forEach { conn ->
                ServerRow(
                    connection = conn,
                    isActive = conn.key() == state.activeKey,
                    onSetActive = { viewModel.setActiveServer(conn.key()) },
                    onRemove = { viewModel.removeServer(conn.key()) },
                )
                HorizontalDivider()
            }

            ListItem(
                headlineContent = { Text("Add Server") },
                leadingContent = { Icon(Icons.Default.Add, contentDescription = null) },
                modifier = Modifier.clickable(onClick = onAddServer),
            )
        }
    }
}

@Composable
private fun ServerRow(
    connection: ServerConnection,
    isActive: Boolean,
    onSetActive: () -> Unit,
    onRemove: () -> Unit,
) {
    var showMenu by remember { mutableStateOf(false) }

    ListItem(
        headlineContent = {
            Text(connection.displayName ?: connection.http.url)
        },
        supportingContent = {
            if (connection.displayName != null) {
                Text(connection.http.url, fontFamily = FontFamily.Monospace)
            }
        },
        leadingContent = {
            if (isActive) Icon(Icons.Default.CheckCircle, contentDescription = "Active", tint = MaterialTheme.colorScheme.primary)
            else Icon(Icons.Default.RadioButtonUnchecked, contentDescription = null)
        },
        trailingContent = {
            Box {
                IconButton(onClick = { showMenu = true }) {
                    Icon(Icons.Default.MoreVert, contentDescription = "More")
                }
                DropdownMenu(expanded = showMenu, onDismissRequest = { showMenu = false }) {
                    if (!isActive) {
                        DropdownMenuItem(
                            text = { Text("Set as active") },
                            onClick = { onSetActive(); showMenu = false },
                        )
                    }
                    DropdownMenuItem(
                        text = { Text("Remove") },
                        onClick = { onRemove(); showMenu = false },
                    )
                }
            }
        },
        modifier = Modifier.clickable(onClick = onSetActive),
    )
}
