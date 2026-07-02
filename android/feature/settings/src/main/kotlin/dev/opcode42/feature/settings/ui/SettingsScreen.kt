package dev.opcode42.feature.settings.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.text.font.FontFamily
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
            HorizontalDivider()
            ServersSection(state, viewModel, onAddServer)
            HorizontalDivider()
            AboutSection()
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
                                    SettingsSection.Appearance -> Icons.Default.Palette
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
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 24.dp),
            ) {
                Spacer(Modifier.height(24.dp))
                when (selectedSection) {
                    SettingsSection.Appearance -> AppearanceSection(state, viewModel)
                    SettingsSection.Servers -> ServersSection(state, viewModel, onAddServer)
                    SettingsSection.About -> AboutSection()
                }
            }
        }
    }
}

// ── Sections ──────────────────────────────────────────────────────────────────

@Composable
private fun AppearanceSection(state: SettingsUiState, viewModel: SettingsViewModel) {
    SectionHeader("Appearance")
    ThemeModeRow("System", "Follows the OS dark setting", state.themeMode == ThemeMode.System) {
        viewModel.setThemeMode(ThemeMode.System)
    }
    ThemeModeRow("Light", "Always light", state.themeMode == ThemeMode.Light) {
        viewModel.setThemeMode(ThemeMode.Light)
    }
    ThemeModeRow("Dark", "Always dark", state.themeMode == ThemeMode.Dark) {
        viewModel.setThemeMode(ThemeMode.Dark)
    }
}

@Composable
private fun ServersSection(
    state: SettingsUiState,
    viewModel: SettingsViewModel,
    onAddServer: () -> Unit,
) {
    SectionHeader("Servers")
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
    SectionHeader("About")
    ListItem(
        headlineContent = { Text("Opcode42") },
        supportingContent = { Text("Mobile client") },
        leadingContent = { Icon(Icons.Default.Info, contentDescription = null) },
    )
}

@Composable
private fun SectionHeader(title: String) {
    Text(
        text = title,
        style = MaterialTheme.typography.labelLarge,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(start = 0.dp, top = 20.dp, bottom = 8.dp),
    )
}

@Composable
private fun ThemeModeRow(label: String, supporting: String, selected: Boolean, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(label) },
        supportingContent = { Text(supporting) },
        leadingContent = {
            RadioButton(selected = selected, onClick = onClick)
        },
        modifier = Modifier.clickable(onClick = onClick),
    )
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