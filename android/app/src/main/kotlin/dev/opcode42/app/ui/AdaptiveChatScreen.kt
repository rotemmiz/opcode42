package dev.opcode42.app.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.expandHorizontally
import androidx.compose.animation.shrinkHorizontally
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
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
import androidx.window.core.layout.WindowHeightSizeClass
import androidx.window.core.layout.WindowWidthSizeClass
import dev.opcode42.core.model.Session
import dev.opcode42.core.model.SnapshotFileDiff
import dev.opcode42.core.model.TokenUsage
import dev.opcode42.core.design.theme.*
import dev.opcode42.feature.chat.ChatViewModel
import dev.opcode42.feature.chat.DRAFT_SESSION_ID
import dev.opcode42.feature.chat.TodoItem
import dev.opcode42.feature.chat.ui.*
import dev.opcode42.feature.sessions.SessionFilter
import dev.opcode42.feature.sessions.SessionListEvent
import dev.opcode42.feature.sessions.SessionListUiState
import dev.opcode42.feature.sessions.SessionListViewModel
import dev.opcode42.feature.sessions.ui.SessionBrowser
import kotlinx.coroutines.launch

/** How the collapsible left sessions menu is presented. Closed by default in both modes. */
enum class LeftRailMode { Overlay, InlinePush }

/** Resolved chat layout for the current window — see [chatLayoutFor]. */
data class ChatLayout(
    /** Chat fills the whole window (no persistent right panel). */
    val singlePane: Boolean,
    /** The right session-info panel is shown persistently ("always available"). */
    val showRightPanel: Boolean,
    /** Presentation of the collapsible left sessions menu (always closed by default). */
    val leftRailMode: LeftRailMode,
)

/**
 * Derive the chat layout from the window's width (height is taken for future
 * height-aware tuning, e.g. relaxing the right panel on a cramped phone-landscape).
 *
 * One rule keyed on width: a **compact-width** window (phone portrait, folded cover
 * display, narrow split-screen) is single-pane with an **overlay** sessions drawer
 * and no right panel; any **wider** window (phone landscape, foldable, tablet — either
 * orientation) shows the persistent right info panel with an **inline-push**,
 * collapsible left rail. The left menu is closed by default in every case.
 *
 * Because we key off the *window* size class (not the physical device), split-screen,
 * freeform, DeX and desktop windows re-evaluate the same rule automatically.
 */
internal fun chatLayoutFor(
    width: WindowWidthSizeClass,
    @Suppress("UNUSED_PARAMETER") height: WindowHeightSizeClass,
): ChatLayout {
    val compactWidth = width == WindowWidthSizeClass.COMPACT
    return ChatLayout(
        singlePane = compactWidth,
        showRightPanel = !compactWidth,
        leftRailMode = if (compactWidth) LeftRailMode.Overlay else LeftRailMode.InlinePush,
    )
}

/**
 * Adaptive layout host for the chat experience. Layout is inferred by [chatLayoutFor]
 * from the window's width + height size class.
 *
 * The left sessions menu is **closed by default** on every form factor — an overlay
 * drawer on compact width, an inline-push rail on wider windows — and surfaces a
 * per-session spinner plus inline permission/question actions so a background session
 * can be answered from the menu itself. The right info panel is persistent on every
 * window wider than compact. System-bar insets are consumed once here; inner panes opt
 * out via applySystemInsets=false. TODOs move from the chat dock to the info panel
 * whenever the panel is visible.
 */
@OptIn(ExperimentalMaterial3AdaptiveApi::class)
@Composable
fun AdaptiveChatScreen(
    sessionId: String,
    onNavigateBack: () -> Unit,
    onOpenTerminal: (String) -> Unit,
    onNavigateToSession: (String) -> Unit,
    onNewSession: () -> Unit = {},
    onOpenTasksBoard: () -> Unit = {},
    chatViewModel: ChatViewModel = hiltViewModel(),
    sessionListViewModel: SessionListViewModel = hiltViewModel(),
) {
    val sessionListState by sessionListViewModel.uiState.collectAsStateWithLifecycle()
    val chatUiState by chatViewModel.uiState.collectAsStateWithLifecycle()

    // Session-list action errors (rename/archive/delete/reply/… from the rail) surface here in
    // multi-pane — the single-pane SessionListScreen isn't composed in this host, so without this
    // collector those events would have no consumer. (Chat-side errors are handled by ChatScreen.)
    val sessionSnackbar = remember { SnackbarHostState() }
    LaunchedEffect(Unit) {
        sessionListViewModel.events.collect { event ->
            when (event) {
                is SessionListEvent.ShowError -> sessionSnackbar.showSnackbar(event.message)
            }
        }
    }
    // Multi-pane has no full-screen session-list error surface (that lives in single-pane
    // SessionListScreen), so a catastrophic load failure is surfaced here as a snackbar instead.
    LaunchedEffect(sessionListState.error) {
        sessionListState.error?.let { sessionSnackbar.showSnackbar(it) }
    }

    val windowSize = currentWindowAdaptiveInfo().windowSizeClass
    val layout = remember(windowSize.windowWidthSizeClass, windowSize.windowHeightSizeClass) {
        chatLayoutFor(windowSize.windowWidthSizeClass, windowSize.windowHeightSizeClass)
    }

    val scope = rememberCoroutineScope()
    val drawerState = rememberDrawerState(DrawerValue.Closed)
    // Inline-push rail visibility. Closed by default; re-keyed on layout so a change in
    // layout (e.g. fold/unfold across the compact boundary) resets it to closed rather
    // than reopening with stale state. The effect closes the overlay drawer on the same
    // change so it never lingers when the layout switches out of overlay mode.
    var railOpen by remember(layout) { mutableStateOf(false) }
    LaunchedEffect(layout) {
        if (drawerState.isOpen) drawerState.close()
    }

    // Right info panel: collapsible, but ON by default. Re-keyed on layout so it returns
    // to open when the layout changes. Only meaningful when the layout has a right panel.
    var infoPanelOpen by remember(layout) { mutableStateOf(true) }
    val rightPanelVisible = layout.showRightPanel && infoPanelOpen

    // Badge the menu button when a session *other than the current one* needs the user,
    // so a background permission/question is noticeable while the menu is collapsed.
    val needsAttentionElsewhere = remember(
        sessionListState.pendingPermissions,
        sessionListState.pendingQuestions,
        sessionId,
    ) {
        (sessionListState.pendingPermissions.keys + sessionListState.pendingQuestions.keys)
            .any { it != sessionId }
    }

    val toggleMenu: () -> Unit = {
        when (layout.leftRailMode) {
            LeftRailMode.Overlay -> scope.launch {
                if (drawerState.isOpen) drawerState.close() else drawerState.open()
            }
            LeftRailMode.InlinePush -> railOpen = !railOpen
        }
    }

    val railPane: @Composable (Modifier, () -> Unit) -> Unit = { mod, onDone ->
        NavRailPane(
            uiState = sessionListState,
            activeSessionId = sessionId,
            activeDirectory = chatUiState.session?.directory,
            onSelectSession = { id -> onDone(); onNavigateToSession(id) },
            onNewSession = {
                onDone()
                onNewSession()
            },
            onQueryChange = sessionListViewModel::setQuery,
            onFilterChange = sessionListViewModel::setFilter,
            onRename = sessionListViewModel::renameSession,
            onArchive = sessionListViewModel::archiveSession,
            onFork = { id -> sessionListViewModel.forkSession(id) { onNavigateToSession(it.id) } },
            onDelete = sessionListViewModel::deleteSession,
            onReplyPermission = sessionListViewModel::replyPermission,
            onReplyQuestion = sessionListViewModel::replyQuestion,
            onSkipQuestion = sessionListViewModel::rejectQuestion,
            onCollapse = onDone,
            modifier = mod,
        )
    }

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

    // Center pane: chat, plus the persistent right info panel when the layout calls for it.
    val centerPane: @Composable (Modifier) -> Unit = { mod ->
        Row(mod) {
            // Box wrapper gives ChatScreen's Scaffold a bounded weight slot in the Row.
            Box(Modifier.weight(1f)) {
                ChatScreen(
                    sessionId = sessionId,
                    onNavigateBack = onNavigateBack,
                    onOpenTerminal = onOpenTerminal,
                    onNavigateToSession = onNavigateToSession,
                    onNewSession = onNewSession,
                    onOpenTasksBoard = onOpenTasksBoard,
                    applySystemInsets = false,
                    isMultiPane = !layout.singlePane,
                    onOpenNavRail = toggleMenu,
                    attentionBadge = needsAttentionElsewhere,
                    showInfoToggle = layout.showRightPanel,
                    infoPanelOpen = infoPanelOpen,
                    onToggleInfoPanel = { infoPanelOpen = !infoPanelOpen },
                    showTodoSheet = !rightPanelVisible,
                    isDraft = sessionId == DRAFT_SESSION_ID,
                    viewModel = chatViewModel,
                )
            }
            if (rightPanelVisible) {
                Box(Modifier.width(1.dp).fillMaxHeight().background(Hairline))
                SessionInfoPanel(
                    session = chatUiState.session,
                    agentMode = chatUiState.agentMode,
                    modelID = chatUiState.modelID,
                    providerID = chatUiState.providerID,
                    tokens = chatUiState.contextTokens,
                    todos = chatUiState.todos,
                    diffs = aggregatedDiffs,
                    modifier = Modifier.width(280.dp).fillMaxHeight(),
                )
            }
        }
    }

    Box(
        Modifier
            .fillMaxSize()
            .background(Surface)
            .systemBarsPadding(),
    ) {
        when (layout.leftRailMode) {
            // Compact width: the sessions menu floats over the chat as a scrim drawer.
            LeftRailMode.Overlay -> ModalNavigationDrawer(
                drawerState = drawerState,
                drawerContent = {
                    ModalDrawerSheet(Modifier.width(300.dp), drawerContainerColor = Surface) {
                        railPane(Modifier.fillMaxSize()) { scope.launch { drawerState.close() } }
                    }
                },
            ) {
                centerPane(Modifier.fillMaxSize())
            }
            // Wider windows: the menu pushes the chat aside (inline), closed by default.
            LeftRailMode.InlinePush -> Row(Modifier.fillMaxSize()) {
                AnimatedVisibility(
                    visible = railOpen,
                    enter = slideInHorizontally { -it } + expandHorizontally(expandFrom = Alignment.Start),
                    exit = slideOutHorizontally { -it } + shrinkHorizontally(shrinkTowards = Alignment.Start),
                ) {
                    Row(Modifier.fillMaxHeight()) {
                        railPane(Modifier.width(220.dp).fillMaxHeight()) { railOpen = false }
                        Box(Modifier.width(1.dp).fillMaxHeight().background(Hairline))
                    }
                }
                centerPane(Modifier.weight(1f))
            }
        }

        SnackbarHost(sessionSnackbar, Modifier.align(Alignment.BottomCenter))
    }
}

// ─── Left nav rail pane ────────────────────────────────────────────────────────

@Composable
internal fun NavRailPane(
    uiState: SessionListUiState,
    activeSessionId: String,
    activeDirectory: String?,
    onSelectSession: (String) -> Unit,
    onNewSession: () -> Unit,
    onQueryChange: (String) -> Unit,
    onFilterChange: (SessionFilter) -> Unit,
    onRename: (String, String) -> Unit,
    onArchive: (String) -> Unit,
    onFork: (String) -> Unit,
    onDelete: (String) -> Unit,
    onReplyPermission: (String, Boolean) -> Unit,
    onReplyQuestion: (String, String) -> Unit,
    onSkipQuestion: (String) -> Unit,
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
                text = "opcode42",
                fontFamily = Opcode42Mono,
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

        SessionBrowser(
            uiState = uiState,
            activeSessionId = activeSessionId,
            onOpen = { session -> onSelectSession(session.id) },
            onQueryChange = onQueryChange,
            onFilterChange = onFilterChange,
            onRename = onRename,
            onArchive = onArchive,
            onFork = onFork,
            onDelete = onDelete,
            onReplyPermission = onReplyPermission,
            onReplyQuestion = onReplyQuestion,
            onSkipQuestion = onSkipQuestion,
            compact = true,
            modifier = Modifier.weight(1f),
        )

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
                    fontFamily = Opcode42Mono,
                    fontSize = 11.sp,
                    color = OnSurfaceFaint,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
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
        // The model + agent-mode are already shown in the chat top bar, so the panel
        // skips a redundant header and starts straight at the SESSION section.
        Spacer(Modifier.height(8.dp))

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
            val used = tokens.contextFootprint
            val fraction = (used.toFloat() / 200_000L).coerceIn(0f, 1f)
            InfoSectionHeader("CONTEXT")
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 1.dp),
            ) {
                Text(
                    text = formatTokens(used),
                    fontFamily = Opcode42Mono,
                    fontSize = 12.5.sp,
                    color = OnSurfaceVariant,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    text = "/ 200K · ${(fraction * 100).toInt()}%",
                    fontFamily = Opcode42Mono,
                    fontSize = 12.5.sp,
                    color = OnSurfaceFaint,
                )
            }
            LinearProgressIndicator(
                progress = { fraction },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 10.dp, vertical = 3.dp)
                    .height(4.dp),
                color = Primary,
                trackColor = OutlineVariant,
            )
        }

        if (diffs.isNotEmpty()) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.fillMaxWidth()
                    .padding(start = 10.dp, end = 10.dp, top = 7.dp, bottom = 2.dp),
            ) {
                Text(
                    text = "CHANGES",
                    fontSize = 11.5.sp,
                    letterSpacing = 0.1.em,
                    fontWeight = FontWeight.SemiBold,
                    color = Secondary,
                    fontFamily = Opcode42Mono,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    text = "${diffs.size} files",
                    fontSize = 11.5.sp,
                    color = OnSurfaceFaint,
                    fontFamily = Opcode42Mono,
                )
            }
            diffs.forEach { diff ->
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 1.dp),
                ) {
                    Text(
                        text = diff.file?.substringAfterLast('/') ?: "unknown",
                        fontFamily = Opcode42Mono,
                        fontSize = 12.5.sp,
                        color = OnSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    Spacer(Modifier.width(6.dp))
                    if (diff.additions > 0) {
                        Text(
                            text = "+${diff.additions}",
                            fontFamily = Opcode42Mono,
                            fontSize = 12.5.sp,
                            color = Tertiary,
                        )
                        Spacer(Modifier.width(3.dp))
                    }
                    if (diff.deletions > 0) {
                        Text(
                            text = "-${diff.deletions}",
                            fontFamily = Opcode42Mono,
                            fontSize = 12.5.sp,
                            color = Error,
                        )
                    }
                }
            }
            Spacer(Modifier.height(3.dp))
        }

        if (todos.isNotEmpty()) {
            InfoSectionHeader("TODOS")
            todos.forEach { todo ->
                Row(
                    verticalAlignment = Alignment.Top,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 1.dp),
                ) {
                    val (dot, dotColor) = when (todo.status) {
                        "completed" -> "✓" to Tertiary
                        "in_progress" -> "→" to Secondary
                        else -> "·" to OnSurfaceFaint
                    }
                    Text(
                        text = dot,
                        fontFamily = Opcode42Mono,
                        fontSize = 12.5.sp,
                        color = dotColor,
                        modifier = Modifier.width(16.dp),
                    )
                    Spacer(Modifier.width(4.dp))
                    Text(
                        text = todo.content,
                        fontSize = 13.5.sp,
                        color = if (todo.status == "completed") OnSurfaceFaint else OnSurfaceVariant,
                        lineHeight = 18.sp,
                    )
                }
            }
            Spacer(Modifier.height(3.dp))
        }

        Spacer(Modifier.height(12.dp))
    }
}

@Composable
private fun InfoSectionHeader(label: String) {
    Text(
        text = label,
        modifier = Modifier.padding(start = 10.dp, end = 10.dp, top = 7.dp, bottom = 2.dp),
        fontSize = 11.5.sp,
        letterSpacing = 0.1.em,
        fontWeight = FontWeight.SemiBold,
        color = Secondary,
        fontFamily = Opcode42Mono,
    )
}

@Composable
private fun InfoRow(key: String, value: String) {
    Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 1.dp)) {
        Text(
            text = key,
            fontFamily = Opcode42Mono,
            fontSize = 12.5.sp,
            color = OnSurfaceFaint,
            modifier = Modifier.width(64.dp),
        )
        Text(
            text = value,
            fontFamily = Opcode42Mono,
            fontSize = 12.5.sp,
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
