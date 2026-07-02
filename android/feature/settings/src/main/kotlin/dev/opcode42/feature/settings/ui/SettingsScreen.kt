package dev.opcode42.feature.settings.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.BrightnessMedium
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.outlined.Palette
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.opcode42.feature.connections.ServerConnection
import dev.opcode42.feature.settings.SettingsUiState
import dev.opcode42.feature.settings.SettingsViewModel
import dev.opcode42.feature.settings.ThemeMode

/** A settings section — the left-pane entry; [content] renders in the right pane on wide screens. */
private enum class SettingsSection(val title: String) {
    Appearance("Appearance"),
    Servers("Servers"),
    About("About"),
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    onNavigateBack: () -> Unit,
    onAddServer: () -> Unit,
    viewModel: SettingsViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    // Two-pane on >=600dp (Material MEDIUM width breakpoint), matching the chat layout tiers.
    val isTwoPane = LocalConfiguration.current.screenWidthDp >= 600

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
            AboutSection()
            Spacer(Modifier.height(24.dp))
        }
    }
}

// ── Two-pane (tablet/fold) — list on left, content on right ────────────────────
// The native Android settings pattern: a fixed-width section index on the left, the
// selected section's content scrolls on the right. A selection highlight marks the
// active section. This matches Android System Settings on tablets/foldables.

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
            // Left pane: section index (fixed 240dp) — the native Android settings master-detail.
            NavigationRail(
                modifier = Modifier.width(240.dp).fillMaxHeight(),
                containerColor = MaterialTheme.colorScheme.surfaceContainerLow,
            ) {
                SettingsSection.entries.forEach { section ->
                    val selected = section == selectedSection
                    NavigationRailItem(
                        selected = selected,
                        onClick = { selectedSection = section },
                        icon = {
                            Icon(
                                when (section) {
                                    SettingsSection.Appearance -> Icons.Outlined.Palette
                                    SettingsSection.Servers -> Icons.Default.Dns
                                    SettingsSection.About -> Icons.Default.Info
                                },
                                contentDescription = null,
                            )
                        },
                        label = { Text(section.title) },
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
                    SettingsSection.About -> AboutSection()
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

// ── Sections ──────────────────────────────────────────────────────────────────

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
private fun ServersSection(
    state: SettingsUiState,
    viewModel: SettingsViewModel,
    onAddServer: () -> Unit,
) {
    CategoryHeader("Servers")
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

@Composable
private fun AboutSection() {
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
            Text(if (version.isNotBlank()) "Mobile client • $version" else "Mobile client")
        },
        leadingContent = { Icon(Icons.Default.Info, contentDescription = null) },
    )
}

// ── Native Android Settings category header ───────────────────────────────────
// Matches Android System Settings' small colored category label: uppercase,
// labelMedium, primary color, 16dp horizontal padding, generous top inset.

@Composable
private fun CategoryHeader(title: String) {
    Text(
        text = title.uppercase(),
        style = MaterialTheme.typography.labelMedium,
        fontWeight = FontWeight.Bold,
        color = MaterialTheme.colorScheme.primary,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 24.dp, bottom = 8.dp),
    )
}

// ── Theme picker dialog ───────────────────────────────────────────────────────
// Native Android Settings "Theme" pattern: a dialog listing the three options as
// ListItems with a check on the current selection.

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
                    ListItem(
                        headlineContent = { Text(themeModeLabel(mode)) },
                        leadingContent = {
                            if (mode == currentMode) {
                                Icon(Icons.Default.Check, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
                            } else {
                                Spacer(Modifier.size(24.dp))
                            }
                        },
                        modifier = Modifier.clickable { onSelect(mode) },
                    )
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
            Box(
                modifier = Modifier
                    .size(10.dp)
                    .clip(CircleShape)
                    .background(
                        if (isActive) MaterialTheme.colorScheme.primary
                        else MaterialTheme.colorScheme.outlineVariant
                    ),
            )
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

private fun themeModeLabel(mode: ThemeMode): String = when (mode) {
    ThemeMode.System -> "System default"
    ThemeMode.Light -> "Light"
    ThemeMode.Dark -> "Dark"
}