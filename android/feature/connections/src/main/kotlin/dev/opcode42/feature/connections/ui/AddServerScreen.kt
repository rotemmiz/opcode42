package dev.opcode42.feature.connections.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.Lan
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.opcode42.feature.connections.ConnectionsViewModel

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
    var directory by remember { mutableStateOf("") }
    var showHelp by remember { mutableStateOf(false) }

    // Discover nearby servers while adding — tap to autofill the URL field.
    val discovered by viewModel.discoveredServers.collectAsStateWithLifecycle()
    DisposableEffect(Unit) {
        viewModel.mdnsDiscovery.start()
        onDispose { viewModel.mdnsDiscovery.stop() }
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
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
            contentAlignment = Alignment.TopCenter,
        ) {
            Column(
                modifier = Modifier
                    .widthIn(max = 480.dp)
                    .fillMaxHeight()
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 16.dp),
            ) {
                Spacer(Modifier.height(8.dp))

                OutlinedTextField(
                    value = url,
                    onValueChange = { url = it },
                    label = { Text("Server URL") },
                    placeholder = { Text("http://192.168.1.10:4096") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                    modifier = Modifier.fillMaxWidth(),
                )

                Spacer(Modifier.height(12.dp))

                OutlinedTextField(
                    value = username,
                    onValueChange = { username = it },
                    label = { Text("Username (optional)") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )

                Spacer(Modifier.height(12.dp))

                OutlinedTextField(
                    value = password,
                    onValueChange = { password = it },
                    label = { Text("Password (optional)") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                    modifier = Modifier.fillMaxWidth(),
                )

                Spacer(Modifier.height(12.dp))

                OutlinedTextField(
                    value = displayName,
                    onValueChange = { displayName = it },
                    label = { Text("Display name (optional)") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )

                Spacer(Modifier.height(12.dp))

                OutlinedTextField(
                    value = directory,
                    onValueChange = { directory = it },
                    label = { Text("Working directory (optional)") },
                    placeholder = { Text("Leave blank to see all projects") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )

                // Nearby servers (mDNS) — M3 ListItems; tap to autofill the URL field.
                if (discovered.isNotEmpty()) {
                    Text(
                        "Nearby servers",
                        style = MaterialTheme.typography.labelMedium,
                        fontWeight = FontWeight.Bold,
                        color = MaterialTheme.colorScheme.primary,
                        modifier = Modifier.padding(start = 0.dp, top = 8.dp, bottom = 4.dp),
                    )
                    discovered.forEach { server ->
                        ListItem(
                            headlineContent = { Text(server.name) },
                            supportingContent = { Text(server.url) },
                            leadingContent = { Icon(Icons.Default.Dns, contentDescription = null) },
                            trailingContent = { Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = null) },
                            modifier = Modifier.clickable { url = server.url },
                        )
                        HorizontalDivider()
                    }
                }

                // "How to run a server" — expandable ListItem with supporting text.
                ListItem(
                    headlineContent = { Text("How to run a server") },
                    leadingContent = { Icon(Icons.Default.Lan, contentDescription = null) },
                    modifier = Modifier.clickable { showHelp = !showHelp },
                )
                if (showHelp) {
                    Surface(
                        color = MaterialTheme.colorScheme.surfaceContainerLow,
                        shape = MaterialTheme.shapes.medium,
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Column(Modifier.padding(16.dp)) {
                            Text("Run the daemon on this machine or another:", style = MaterialTheme.typography.labelLarge)
                            Spacer(Modifier.height(8.dp))
                            Text("opcoded serve --hostname 0.0.0.0 --port 4096", style = MaterialTheme.typography.bodySmall)
                            Spacer(Modifier.height(4.dp))
                            Text("or (reference daemon):", style = MaterialTheme.typography.labelMedium)
                            Text("opencode serve --mdns --hostname 0.0.0.0", style = MaterialTheme.typography.bodySmall)
                        }
                    }
                }

                Spacer(Modifier.height(4.dp))

                Button(
                    onClick = {
                        if (url.isNotBlank()) {
                            viewModel.addServer(
                                rawUrl = url.trim(),
                                username = username.takeIf { it.isNotBlank() },
                                password = password.takeIf { it.isNotBlank() },
                                displayName = displayName.takeIf { it.isNotBlank() },
                                directory = directory.takeIf { it.isNotBlank() },
                            )
                            onNavigateBack()
                        }
                    },
                    modifier = Modifier.fillMaxWidth(),
                    enabled = url.isNotBlank(),
                ) {
                    Text("Add Server")
                }

                Spacer(Modifier.height(24.dp))
            }
        }
    }
}