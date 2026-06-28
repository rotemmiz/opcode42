package dev.forge.app.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.expandHorizontally
import androidx.compose.animation.shrinkHorizontally
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.*
import androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi
import androidx.compose.material3.adaptive.currentWindowAdaptiveInfo
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.window.core.layout.WindowWidthSizeClass
import dev.forge.core.model.Session
import dev.forge.core.model.SnapshotFileDiff
import dev.forge.core.model.TokenUsage
import dev.forge.feature.chat.ChatViewModel
import dev.forge.feature.chat.TodoItem
import dev.forge.feature.chat.ui.*
import dev.forge.feature.sessions.SessionListViewModel

/**
 * Adaptive layout host for the chat experience. Layout is driven entirely by
 * WindowWidthSizeClass from the framework — no hardcoded dp breakpoints:
 *
 *   COMPACT  (phone portrait, closed foldable)  → single-pane: ChatScreen only
 *   MEDIUM   (open foldable portrait, tablet)   → two-pane: NavRail + Chat
 *   EXPANDED (open foldable landscape, desktop) → three-pane: NavRail + Chat + InfoPanel
 *
 * System-bar insets are consumed once here; inner panes opt out via applySystemInsets=false.
 * The nav rail can be toggled in medium/expanded; it auto-resets on fold/unfold.
 * TODOs move from the chat dock to the info panel when the info panel is visible.
 */
@OptIn(ExperimentalMaterial3AdaptiveApi::class)
@Composable
fun AdaptiveChatScreen(
    sessionId: String,
    isDarkTheme: Boolean,
    onToggleTheme: () -> Unit,
    onNavigateBack: () -> Unit,
    onOpenTerminal: (String) -> Unit,
    onNavigateToSession: (String) -> Unit,
    onOpenTasksBoard: () -> Unit = {},
    chatViewModel: ChatViewModel = hiltViewModel(),
    sessionListViewModel: SessionListViewModel = hiltViewModel(),
) {
    val sessionListState by sessionListViewModel.uiState.collectAsStateWithLifecycle()
    val chatUiState by chatViewModel.uiState.collectAsStateWithLifecycle()

    val widthClass = currentWindowAdaptiveInfo().windowSizeClass.windowWidthSizeClass
    val isCompact = widthClass == WindowWidthSizeClass.COMPACT
    val isExpanded = widthClass == WindowWidthSizeClass.EXPANDED

    // Rail auto-shows on non-compact windows. remember(isCompact) resets on fold/unfold
    // so opening the device always reveals the rail without stale user-collapsed state.
    var railVisible by remember(isCompact) { mutableStateOf(!isCompact) }
    val showRail = !isCompact && railVisible
    // Info panel (right) is only shown in expanded (foldable open landscape / large tablet).
    val showInfoPanel = isExpanded

    val aggregatedDiffs = remember(chatUiState.diffs) {
        chatUiState.diffs.values
            .flatten()
            .groupBy { it.file }
            .map { (_, entries) ->
                entries.reduce { acc, diff ->
                    acc.copy(additions = acc.additions + diff.additions, deletions = acc.deletions + diff.deletions)
                }
            }
            .sortedByDescending { it.additions + it.deletions }
    }

    Box(
        Modifier
            .fillMaxSize()
            .background(Surface)
            .systemBarsPadding(),
    ) {
        Row(Modifier.fillMaxSize()) {
            // ── Left: nav rail (non-compact, toggleable) ─────────────────────────
            AnimatedVisibility(
                visible = showRail,
                enter = slideInHorizontally { -it } + expandHorizontally(expandFrom = Alignment.Start),
                exit = slideOutHorizontally { -it } + shrinkHorizontally(shrinkTowards = Alignment.Start),
            ) {
                Row(Modifier.fillMaxHeight()) {
                    NavRailPane(
                        sessions = sessionListState.sessions,
                        activeSessionId = sessionId,
                        activeDirectory = chatUiState.session?.directory,
                        onSelectSession = onNavigateToSession,
                        onNewSession = {
                            sessionListViewModel.createSession { session ->
                                onNavigateToSession(session.id)
                            }
                        },
                        onCollapse = { railVisible = false },
                        modifier = Modifier.width(220.dp).fillMaxHeight(),
                    )
                    Box(Modifier.width(1.dp).fillMaxHeight().background(Hairline))
                }
            }

            // ── Center: chat ──────────────────────────────────────────────────────
            // Box wrapper gives ChatScreen's Scaffold a bounded weight slot in the Row.
            Box(Modifier.weight(1f)) {
                ChatScreen(
                    sessionId = sessionId,
                    onNavigateBack = onNavigateBack,
                    onOpenTerminal = onOpenTerminal,
                    onNavigateToSession = onNavigateToSession,
                    onOpenTasksBoard = onOpenTasksBoard,
                    isDarkTheme = isDarkTheme,
                    onToggleTheme = onToggleTheme,
                    applySystemInsets = false,
                    // Hamburger (vs back arrow) whenever the window is non-compact,
                    // regardless of whether the rail is currently collapsed.
                    isMultiPane = !isCompact,
                    onOpenNavRail = { railVisible = !railVisible },
                    showTodoSheet = !showInfoPanel,
                    viewModel = chatViewModel,
                )
            }

            // ── Right: info panel (expanded only) ─────────────────────────────────
            if (showInfoPanel) {
                Box(Modifier.width(1.dp).fillMaxHeight().background(Hairline))
                SessionInfoPanel(
                    session = chatUiState.session,
                    agentMode = chatUiState.agentMode,
                    modelID = chatUiState.modelID,
                    providerID = chatUiState.providerID,
                    tokens = chatUiState.session?.tokens,
                    todos = chatUiState.todos,
                    diffs = aggregatedDiffs,
                    modifier = Modifier.width(280.dp).fillMaxHeight(),
                )
            }
        }
    }
}

// ─── Left nav rail pane ────────────────────────────────────────────────────────

@Composable
internal fun NavRailPane(
    sessions: List<Session>,
    activeSessionId: String,
    activeDirectory: String?,
    onSelectSession: (String) -> Unit,
    onNewSession: () -> Unit,
    onCollapse: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier.fillMaxSize().background(Surface)) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .fillMaxWidth()
                .height(52.dp)
                .padding(horizontal = 10.dp),
        ) {
            Text(
                text = "forge",
                fontFamily = ForgeMono,
                fontWeight = FontWeight.Bold,
                fontSize = 15.sp,
                color = OnSurface,
                modifier = Modifier.weight(1f),
            )
            FilledTonalButton(
                onClick = onNewSession,
                contentPadding = PaddingValues(horizontal = 8.dp, vertical = 0.dp),
                modifier = Modifier.height(28.dp),
            ) {
                Icon(Icons.Default.Add, contentDescription = null, modifier = Modifier.size(14.dp))
                Spacer(Modifier.width(3.dp))
                Text("New", fontSize = 12.sp)
            }
            Spacer(Modifier.width(4.dp))
            IconButton(onClick = onCollapse, modifier = Modifier.size(32.dp)) {
                Icon(
                    Icons.AutoMirrored.Filled.KeyboardArrowLeft,
                    contentDescription = "Collapse navigation",
                    tint = OnSurfaceFaint,
                    modifier = Modifier.size(18.dp),
                )
            }
        }
        HorizontalDivider(color = Hairline, thickness = 1.dp)

        Text(
            text = "SESSIONS",
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 7.dp),
            fontSize = 10.sp,
            letterSpacing = 0.1.em,
            fontWeight = FontWeight.SemiBold,
            color = Secondary,
            fontFamily = ForgeMono,
        )

        LazyColumn(Modifier.weight(1f)) {
            items(sessions, key = { it.id }) { session ->
                SessionRailRow(
                    session = session,
                    isActive = session.id == activeSessionId,
                    onClick = { onSelectSession(session.id) },
                )
            }
        }

        if (activeDirectory != null) {
            HorizontalDivider(color = Hairline, thickness = 1.dp)
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 10.dp, vertical = 8.dp),
            ) {
                Box(
                    Modifier
                        .size(6.dp)
                        .clip(CircleShape)
                        .background(Tertiary),
                )
                Spacer(Modifier.width(6.dp))
                val home = System.getProperty("user.home")?.takeIf { it.isNotEmpty() } ?: ""
                val displayDir = if (home.isNotEmpty() && activeDirectory.startsWith(home)) {
                    "~" + activeDirectory.removePrefix(home)
                } else {
                    activeDirectory
                }
                Text(
                    text = displayDir,
                    fontFamily = ForgeMono,
                    fontSize = 11.sp,
                    color = OnSurfaceFaint,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

@Composable
private fun SessionRailRow(session: Session, isActive: Boolean, onClick: () -> Unit) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .background(if (isActive) SurfaceContainerHigh else Surface)
            .padding(horizontal = 12.dp, vertical = 7.dp),
    ) {
        Text(
            text = session.title?.takeIf { it.isNotBlank() } ?: "New session",
            fontSize = 13.sp,
            fontWeight = if (isActive) FontWeight.Medium else FontWeight.Normal,
            color = if (isActive) OnSurface else OnSurfaceVariant,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            lineHeight = 16.sp,
        )
    }
}

// ─── Right extra pane — session info panel ─────────────────────────────────────

@Composable
internal fun SessionInfoPanel(
    session: Session?,
    agentMode: String?,
    modelID: String?,
    providerID: String?,
    tokens: TokenUsage?,
    todos: List<TodoItem>,
    diffs: List<SnapshotFileDiff> = emptyList(),
    modifier: Modifier = Modifier,
) {
    Column(
        modifier
            .fillMaxSize()
            .background(SurfaceContainerLowest)
            .verticalScroll(rememberScrollState()),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .fillMaxWidth()
                .height(52.dp)
                .padding(horizontal = 12.dp),
        ) {
            if (agentMode != null) {
                Text(
                    text = agentMode.replaceFirstChar { it.uppercase() },
                    fontFamily = ForgeMono,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.Bold,
                    color = OnPrimary,
                    modifier = Modifier
                        .clip(ForgeShapes.xs)
                        .background(Primary)
                        .padding(horizontal = 6.dp, vertical = 2.dp),
                )
                Spacer(Modifier.width(8.dp))
            }
            Text(
                text = modelID ?: "",
                fontFamily = ForgeMono,
                fontSize = 12.sp,
                color = OnSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
        }
        HorizontalDivider(color = Hairline, thickness = 1.dp)

        if (session != null) {
            InfoSectionHeader("SESSION")
            InfoRow("title", session.title ?: "Untitled")
            InfoRow("id", session.id.take(8))
            session.directory?.let { InfoRow("dir", it) }
        }

        if (modelID != null || providerID != null) {
            InfoSectionHeader("MODEL")
            providerID?.let { InfoRow("provider", it) }
            modelID?.let { InfoRow("model", it) }
            agentMode?.let { InfoRow("mode", it) }
        }

        if (tokens != null) {
            val total = (tokens.input + tokens.output + tokens.reasoning +
                tokens.cache.read + tokens.cache.write).toLong()
            val fraction = (total.toFloat() / 200_000L).coerceIn(0f, 1f)
            InfoSectionHeader("CONTEXT")
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 3.dp),
            ) {
                Text(
                    text = formatTokens(total),
                    fontFamily = ForgeMono,
                    fontSize = 11.sp,
                    color = OnSurfaceVariant,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    text = "/ 200K · ${(fraction * 100).toInt()}%",
                    fontFamily = ForgeMono,
                    fontSize = 11.sp,
                    color = OnSurfaceFaint,
                )
            }
            LinearProgressIndicator(
                progress = { fraction },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 12.dp, vertical = 4.dp)
                    .height(3.dp),
                color = Primary,
                trackColor = OutlineVariant,
            )
        }

        if (diffs.isNotEmpty()) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.fillMaxWidth()
                    .padding(start = 12.dp, end = 12.dp, top = 12.dp, bottom = 3.dp),
            ) {
                Text(
                    text = "CHANGES",
                    fontSize = 10.sp,
                    letterSpacing = 0.1.em,
                    fontWeight = FontWeight.SemiBold,
                    color = Secondary,
                    fontFamily = ForgeMono,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    text = "${diffs.size} files",
                    fontSize = 10.sp,
                    color = OnSurfaceFaint,
                    fontFamily = ForgeMono,
                )
            }
            diffs.forEach { diff ->
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 1.dp),
                ) {
                    Text(
                        text = diff.file?.substringAfterLast('/') ?: "unknown",
                        fontFamily = ForgeMono,
                        fontSize = 11.sp,
                        color = OnSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    Spacer(Modifier.width(6.dp))
                    if (diff.additions > 0) {
                        Text(
                            text = "+${diff.additions}",
                            fontFamily = ForgeMono,
                            fontSize = 11.sp,
                            color = Tertiary,
                        )
                        Spacer(Modifier.width(3.dp))
                    }
                    if (diff.deletions > 0) {
                        Text(
                            text = "-${diff.deletions}",
                            fontFamily = ForgeMono,
                            fontSize = 11.sp,
                            color = Error,
                        )
                    }
                }
            }
            Spacer(Modifier.height(4.dp))
        }

        if (todos.isNotEmpty()) {
            InfoSectionHeader("TODOS")
            todos.forEach { todo ->
                Row(
                    verticalAlignment = Alignment.Top,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 2.dp),
                ) {
                    val (dot, dotColor) = when (todo.status) {
                        "completed" -> "✓" to Tertiary
                        "in_progress" -> "→" to Secondary
                        else -> "·" to OnSurfaceFaint
                    }
                    Text(
                        text = dot,
                        fontFamily = ForgeMono,
                        fontSize = 11.sp,
                        color = dotColor,
                        modifier = Modifier.width(14.dp),
                    )
                    Spacer(Modifier.width(4.dp))
                    Text(
                        text = todo.content,
                        fontSize = 12.sp,
                        color = if (todo.status == "completed") OnSurfaceFaint else OnSurfaceVariant,
                        lineHeight = 16.sp,
                    )
                }
            }
            Spacer(Modifier.height(4.dp))
        }

        Spacer(Modifier.height(16.dp))
    }
}

@Composable
private fun InfoSectionHeader(label: String) {
    Text(
        text = label,
        modifier = Modifier.padding(start = 12.dp, end = 12.dp, top = 12.dp, bottom = 3.dp),
        fontSize = 10.sp,
        letterSpacing = 0.1.em,
        fontWeight = FontWeight.SemiBold,
        color = Secondary,
        fontFamily = ForgeMono,
    )
}

@Composable
private fun InfoRow(key: String, value: String) {
    Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 2.dp)) {
        Text(
            text = key,
            fontFamily = ForgeMono,
            fontSize = 11.sp,
            color = OnSurfaceFaint,
            modifier = Modifier.width(56.dp),
        )
        Text(
            text = value,
            fontFamily = ForgeMono,
            fontSize = 11.sp,
            color = OnSurfaceVariant,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
    }
}

private fun formatTokens(n: Long): String = when {
    n >= 1_000_000 -> "%.1fM".format(n / 1_000_000.0)
    n >= 1_000 -> "%.1fK".format(n / 1_000.0)
    else -> n.toString()
}
