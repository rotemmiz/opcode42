package dev.opcode42.feature.connections.ui

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.opcode42.feature.connections.ConnectionsViewModel

/**
 * First-run connect screen — a purpose-built surface (not the raw AddServer form) shown when
 * no server is configured. A branded hero, a single URL field, optional credentials behind an
 * "Advanced" toggle, and a "How to run a server" expandable. On success, the caller navigates
 * to the chat home and replaces the graph start so back doesn't return here.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ConnectScreen(
    onConnected: () -> Unit,
    viewModel: ConnectionsViewModel = hiltViewModel(),
) {
    var url by remember { mutableStateOf("") }
    var username by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var showAdvanced by remember { mutableStateOf(false) }
    var showHelp by remember { mutableStateOf(false) }

    // Start mDNS discovery on screen show; stop on dispose.
    val discovered by viewModel.discoveredServers.collectAsStateWithLifecycle()
    DisposableEffect(Unit) {
        viewModel.mdnsDiscovery.start()
        onDispose { viewModel.mdnsDiscovery.stop() }
    }

    Scaffold { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                // Cap the form width so it doesn't stretch edge-to-edge on tablet/foldable —
                // a centered ~480dp column reads as a native first-run surface, not a stretched form.
                .widthIn(max = 480.dp)
                .padding(horizontal = 24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            // Hero
            Text(
                text = "opcode42",
                style = MaterialTheme.typography.headlineMedium.copy(fontWeight = FontWeight.Bold),
                letterSpacing = 1.sp,
            )
            Spacer(Modifier.height(8.dp))
            Text(
                text = "Connect to your Opcode42 server",
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(32.dp))

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

            TextButton(
                onClick = { showAdvanced = !showAdvanced },
                modifier = Modifier.align(Alignment.Start),
            ) {
                Text(if (showAdvanced) "Hide advanced" else "Show advanced")
            }

            if (showAdvanced) {
                Spacer(Modifier.height(4.dp))
                OutlinedTextField(
                    value = username,
                    onValueChange = { username = it },
                    label = { Text("Username (optional)") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(8.dp))
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
            }

            Button(
                onClick = {
                    if (url.isNotBlank()) {
                        viewModel.addServer(
                            rawUrl = url.trim(),
                            username = username.takeIf { it.isNotBlank() },
                            password = password.takeIf { it.isNotBlank() },
                        )
                        onConnected()
                    }
                },
                modifier = Modifier.fillMaxWidth(),
                enabled = url.isNotBlank(),
            ) {
                Text("Connect")
            }

            // Nearby servers (mDNS). Tap to autofill the URL field.
            if (discovered.isNotEmpty()) {
                Spacer(Modifier.height(24.dp))
                Text(
                    "Nearby servers",
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.align(Alignment.Start),
                )
                Spacer(Modifier.height(8.dp))
                discovered.forEach { server ->
                    Surface(
                        color = MaterialTheme.colorScheme.surfaceContainerLow,
                        shape = MaterialTheme.shapes.medium,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(vertical = 3.dp),
                        onClick = { url = server.url },
                    ) {
                        Column(Modifier.padding(horizontal = 16.dp, vertical = 10.dp)) {
                            Text(server.name, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
                            Text(server.url, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
            }

            Spacer(Modifier.height(24.dp))

            TextButton(
                onClick = { showHelp = !showHelp },
                modifier = Modifier.align(Alignment.Start),
            ) {
                Text(if (showHelp) "Hide help" else "How to run a server")
            }
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
        }
    }
}