package dev.forge.feature.connections.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.LifecycleStartEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.forge.feature.connections.ConnectionsViewModel
import dev.forge.feature.connections.DiscoveredEntry

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AddServerScreen(
    onNavigateBack: () -> Unit,
    viewModel: ConnectionsViewModel = hiltViewModel(),
) {
    var url by remember { mutableStateOf("") }
    var username by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var displayName by remember { mutableStateOf("") }

    val discovered by viewModel.discovered.collectAsStateWithLifecycle()
    val scanning by viewModel.scanning.collectAsStateWithLifecycle()

    // Browse only while the screen is at least STARTED; drop the multicast lock when it stops.
    LifecycleStartEffect(Unit) {
        viewModel.startDiscovery()
        onStopOrDispose { viewModel.stopDiscovery() }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Add Server") },
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
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Spacer(Modifier.height(8.dp))

            DiscoverySection(
                discovered = discovered,
                scanning = scanning,
                onSelect = { entry ->
                    val server = entry.server
                    if (viewModel.requiresCredentials(server)) {
                        // Daemon wants credentials — prefill the form and let the user finish.
                        url = server.url
                        displayName = server.serviceName
                    } else {
                        viewModel.addDiscovered(server)
                        onNavigateBack()
                    }
                },
            )

            HorizontalDivider()

            Text("Add manually", style = MaterialTheme.typography.titleSmall)

            OutlinedTextField(
                value = url,
                onValueChange = { url = it },
                label = { Text("Server URL") },
                placeholder = { Text("http://192.168.1.10:4096") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                modifier = Modifier.fillMaxWidth(),
            )

            OutlinedTextField(
                value = username,
                onValueChange = { username = it },
                label = { Text("Username (optional)") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )

            OutlinedTextField(
                value = password,
                onValueChange = { password = it },
                label = { Text("Password (optional)") },
                singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                modifier = Modifier.fillMaxWidth(),
            )

            OutlinedTextField(
                value = displayName,
                onValueChange = { displayName = it },
                label = { Text("Display name (optional)") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )

            Spacer(Modifier.height(4.dp))

            Button(
                onClick = {
                    if (url.isNotBlank()) {
                        viewModel.addServer(
                            rawUrl = url.trim(),
                            username = username.takeIf { it.isNotBlank() },
                            password = password.takeIf { it.isNotBlank() },
                            displayName = displayName.takeIf { it.isNotBlank() },
                        )
                        onNavigateBack()
                    }
                },
                modifier = Modifier.fillMaxWidth(),
                enabled = url.isNotBlank(),
            ) {
                Text("Add Server")
            }

            Spacer(Modifier.height(8.dp))
        }
    }
}

@Composable
private fun DiscoverySection(
    discovered: List<DiscoveredEntry>,
    scanning: Boolean,
    onSelect: (DiscoveredEntry) -> Unit,
) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(
            "Servers on your network",
            style = MaterialTheme.typography.titleSmall,
            modifier = Modifier.weight(1f),
        )
        if (scanning) {
            CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
        }
    }

    if (discovered.isEmpty()) {
        Text(
            if (scanning) "Looking for daemons…"
            else "None found. Make sure you're on the same Wi-Fi.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    } else {
        discovered.forEach { entry -> DiscoveredServerRow(entry, onClick = { onSelect(entry) }) }
    }
}

@Composable
private fun DiscoveredServerRow(entry: DiscoveredEntry, onClick: () -> Unit) {
    val server = entry.server
    ListItem(
        modifier = Modifier.clickable(enabled = !entry.alreadyAdded, onClick = onClick),
        leadingContent = if (entry.alreadyAdded) {
            { Icon(Icons.Filled.CheckCircle, contentDescription = "Already added") }
        } else null,
        headlineContent = { Text(server.serviceName) },
        supportingContent = {
            val version = server.version?.let { " · v$it" } ?: ""
            Text("${server.host}:${server.port}$version")
        },
        trailingContent = if (entry.alreadyAdded) {
            { Text("Added", style = MaterialTheme.typography.labelSmall) }
        } else null,
    )
}
