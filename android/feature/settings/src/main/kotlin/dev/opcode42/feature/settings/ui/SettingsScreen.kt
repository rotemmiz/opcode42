package dev.opcode42.feature.settings.ui

import android.os.Build
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.BrightnessMedium
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Palette
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.opcode42.core.store.ConnectionState
import dev.opcode42.feature.connections.ServerConnection
import dev.opcode42.core.design.theme.Opcode42Mono
import dev.opcode42.core.design.theme.Hairline
import dev.opcode42.feature.settings.SettingsUiState
import dev.opcode42.feature.settings.SettingsViewModel
import dev.opcode42.feature.settings.ThemeMode

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    onNavigateBack: () -> Unit,
    onAddServer: () -> Unit,
    viewModel: SettingsViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    val adaptiveInfo = androidx.compose.material3.adaptive.currentWindowAdaptiveInfo()
    val isTwoPane = adaptiveInfo.windowSizeClass.windowWidthSizeClass != androidx.window.core.layout.WindowWidthSizeClass.COMPACT

    if (isTwoPane) {
        TwoPaneSettings(state, viewModel, onNavigateBack, onAddServer)
    } else {
        SinglePaneSettings(state, viewModel, onNavigateBack, onAddServer)
    }
}

// ── Single-pane (phone) — one scrollable list ─────────────────────────────────

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SinglePaneSettings(
    state: SettingsUiState,
    viewModel: SettingsViewModel,
    onNavigateBack: () -> Unit,
    onAddServer: () -> Unit,
) {
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
                .padding(padding)
                .verticalScroll(rememberScrollState()),
        ) {
            AppearanceSection(state, viewModel)
            ServersSection(state, viewModel, onAddServer)
            AboutSection(state)
            Spacer(Modifier.height(24.dp))
        }
    }
}

// ── Two-pane (tablet/fold) — plain list on left, content on right ──────────────
// Matches Android System Settings on tablets: a simple ListItem list as the
// master (not a NavigationRail — that's app-level nav, not settings master-detail),
// with the selected section's content scrolling on the right.

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun TwoPaneSettings(
    state: SettingsUiState,
    viewModel: SettingsViewModel,
    onNavigateBack: () -> Unit,
    onAddServer: () -> Unit,
) {
    var selectedSection by remember { mutableStateOf(SettingsSection.Appearance) }

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
        Row(modifier = Modifier.fillMaxSize().padding(padding)) {
            // Left pane: plain ListItem list (the settings master-detail pattern).
            Column(
                modifier = Modifier
                    .width(280.dp)
                    .fillMaxHeight()
                    .background(MaterialTheme.colorScheme.surfaceContainerLow)
                    .verticalScroll(rememberScrollState()),
            ) {
                SettingsSection.entries.forEach { section ->
                    val selected = section == selectedSection
                    ListItem(
                        headlineContent = { Text(section.title) },
                        leadingContent = {
                            Icon(
                                when (section) {
                                    SettingsSection.Appearance -> Icons.Default.BrightnessMedium
                                    SettingsSection.Servers -> Icons.Default.Dns
                                    SettingsSection.About -> Icons.Default.Info
                                },
                                contentDescription = null,
                                tint = if (selected) MaterialTheme.colorScheme.primary
                                else MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        },
                        colors = ListItemDefaults.colors(
                            containerColor = if (selected) MaterialTheme.colorScheme.surfaceContainer
                            else MaterialTheme.colorScheme.surfaceContainerLow,
                        ),
                        modifier = Modifier.clickable { selectedSection = section },
                    )
                }
            }
            VerticalDivider()
            // Right pane: selected section content (scrollable).
            Column(
                modifier = Modifier
                    .weight(1f)
                    .fillMaxHeight()
                    .verticalScroll(rememberScrollState()),
            ) {
                when (selectedSection) {
                    SettingsSection.Appearance -> AppearanceSection(state, viewModel)
                    SettingsSection.Servers -> ServersSection(state, viewModel, onAddServer)
                    SettingsSection.About -> AboutSection(state)
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

// ── Sections ──────────────────────────────────────────────────────────────────

private enum class SettingsSection(val title: String) {
    Appearance("Appearance"),
    Servers("Servers"),
    About("About"),
}

@Composable
private fun AppearanceSection(state: SettingsUiState, viewModel: SettingsViewModel) {
    var showThemeDialog by remember { mutableStateOf(false) }

    CategoryHeader("Appearance")
    ListItem(
        headlineContent = { Text("Theme") },
        supportingContent = { Text(themeModeLabel(state.themeMode)) },
        leadingContent = { Icon(Icons.Default.BrightnessMedium, contentDescription = null) },
        trailingContent = { Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = null) },
        modifier = Modifier.clickable { showThemeDialog = true },
    )
    HorizontalDivider()

    DynamicColorRow(state.dynamicColor, viewModel::setDynamicColor)

    if (showThemeDialog) {
        ThemePickerDialog(
            currentMode = state.themeMode,
            onSelect = { mode ->
                viewModel.setThemeMode(mode)
                showThemeDialog = false
            },
            onDismiss = { showThemeDialog = false },
        )
    }
}

@Composable
private fun DynamicColorRow(
    enabled: Boolean,
    onChange: (Boolean) -> Unit,
) {
    val supported = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S
    ListItem(
        headlineContent = { Text("Dynamic color (Material You)") },
        supportingContent = {
            Text(if (supported) "Match colors to your wallpaper" else "Requires Android 12+")
        },
        leadingContent = { Icon(Icons.Default.Palette, contentDescription = null) },
        trailingContent = {
            Switch(
                checked = enabled && supported,
                onCheckedChange = onChange,
                enabled = supported,
            )
        },
        modifier = if (supported) {
            Modifier.clickable { onChange(!enabled) }
        } else {
            Modifier
        },
    )
    HorizontalDivider()
}

@Composable
private fun ServersSection(
    state: SettingsUiState,
    viewModel: SettingsViewModel,
    onAddServer: () -> Unit,
) {
    CategoryHeader("Servers")
    state.connections.forEach { conn ->
        val isActive = conn.key() == state.activeKey
        ServerRow(
            connection = conn,
            isActive = isActive,
            connectionState = if (isActive) state.activeConnectionState
                else ConnectionState.Disconnected,
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

@Composable
private fun AboutSection(state: SettingsUiState) {
    val context = LocalContext.current
    val version = remember {
        runCatching {
            context.packageManager.getPackageInfo(context.packageName, 0).versionName
        }.getOrNull() ?: ""
    }

    CategoryHeader("About")
    ListItem(
        headlineContent = { Text("Opcode42") },
        supportingContent = {
            val appLine = if (version.isNotBlank()) "Mobile client • $version" else "Mobile client"
            val daemonLine = state.daemonVersion?.let { "Daemon • $it" }
            Column {
                Text(appLine)
                daemonLine?.let { Text(it) }
            }
        },
        leadingContent = { Icon(Icons.Default.Info, contentDescription = null) },
    )
}

// ── Category header ───────────────────────────────────────────────────────────
// Android Settings style: small muted-grey label, title case, labelMedium.

@Composable
private fun CategoryHeader(title: String) {
    Text(
        text = title,
        style = MaterialTheme.typography.labelMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 24.dp, bottom = 8.dp),
    )
}

// ── Theme picker dialog ───────────────────────────────────────────────────────
// Standard Android dialog pattern: RadioButton in a Column, one per option.

@Composable
private fun ThemePickerDialog(
    currentMode: ThemeMode,
    onSelect: (ThemeMode) -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Theme") },
        text = {
            Column {
                ThemeMode.entries.forEach { mode ->
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable { onSelect(mode) }
                            .padding(vertical = 8.dp),
                    ) {
                        RadioButton(
                            selected = mode == currentMode,
                            onClick = { onSelect(mode) },
                        )
                        Spacer(Modifier.width(8.dp))
                        Text(themeModeLabel(mode))
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onDismiss) { Text("Cancel") }
        },
    )
}

// ── Server row — connection status dot + name + MoreVert ──────────────────────

@Composable
private fun ServerRow(
    connection: ServerConnection,
    isActive: Boolean,
    connectionState: ConnectionState,
    onSetActive: () -> Unit,
    onRemove: () -> Unit,
) {
    var showMenu by remember { mutableStateOf(false) }

    // G1 — Connection-state dot: green (connected), amber (connecting), red (failed),
    // grey (no server / disconnected). Only the active server has a live SSE state;
    // inactive servers show grey.
    val dotColor = when {
        !isActive -> MaterialTheme.colorScheme.outlineVariant
        connectionState is ConnectionState.Connected -> MaterialTheme.colorScheme.tertiary
        connectionState is ConnectionState.Connecting -> MaterialTheme.colorScheme.secondary
        connectionState is ConnectionState.Failed -> MaterialTheme.colorScheme.error
        else -> MaterialTheme.colorScheme.outlineVariant
    }

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
            Box(
                modifier = Modifier
                    .size(10.dp)
                    .clip(CircleShape)
                    .background(dotColor),
            )
        },
        trailingContent = {
            Box {
                IconButton(onClick = { showMenu = true }) {
                    Icon(Icons.Default.MoreVert, contentDescription = "More")
                }
                DropdownMenu(
                    expanded = showMenu,
                    onDismissRequest = { showMenu = false },
                    modifier = Modifier.border(androidx.compose.foundation.BorderStroke(1.dp, Hairline), MaterialTheme.shapes.extraSmall)
                ) {
                    if (!isActive) {
                        DropdownMenuItem(
                            text = { Text("Set as active", fontFamily = Opcode42Mono, fontSize = 13.sp) },
                            onClick = { onSetActive(); showMenu = false },
                        )
                    }
                    DropdownMenuItem(
                        text = { Text("Remove", fontFamily = Opcode42Mono, fontSize = 13.sp, color = MaterialTheme.colorScheme.error) },
                        onClick = { onRemove(); showMenu = false },
                    )
                }
            }
        },
        modifier = Modifier.clickable(onClick = onSetActive),
    )
}

private fun themeModeLabel(mode: ThemeMode): String = when (mode) {
    ThemeMode.System -> "System default"
    ThemeMode.Light -> "Light"
    ThemeMode.Dark -> "Dark"
}