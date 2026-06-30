package dev.opcode42.app.ui

import androidx.activity.ComponentActivity
import androidx.activity.compose.LocalActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.RadioButtonUnchecked
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
import dev.opcode42.core.model.CommandInfo
import dev.opcode42.core.model.Session
import dev.opcode42.core.model.SnapshotFileDiff
import dev.opcode42.core.model.TokenUsage
import dev.opcode42.core.design.brand.Spinner
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
import dev.opcode42.feature.sessions.ui.isSessionBusy
import kotlinx.coroutines.launch

/** How the collapsible left sessions menu is presented. Closed by default in both modes. */
enum class LeftRailMode { Overlay, InlinePush }

/** Resolved chat layout for the current window — see [chatLayoutFor]. */
data class ChatLayout(
    /** Chat fills the whole window (no persistent right panel). */
    val singlePane: Boolean,
    /** The right session-info panel is shown persistently ("always available"). */
    val showRightPanel: Boolean,
    /** Presentation of the collapsible left sessions menu. */
    val leftRailMode: LeftRailMode,
    /** Expanded width: the rail is open by default (the full triptych), not closed. */
    val railPersistent: Boolean,
)

/** The three standard width tiers — Material `WindowWidthSizeClass` breakpoints (600/840dp). */
enum class ChatPaneTier { Compact, Medium, Expanded }

internal fun chatPaneTier(width: WindowWidthSizeClass): ChatPaneTier = when (width) {
    WindowWidthSizeClass.COMPACT -> ChatPaneTier.Compact
    WindowWidthSizeClass.MEDIUM -> ChatPaneTier.Medium
    else -> ChatPaneTier.Expanded
}

/**
 * Derive the chat layout from the window's **width size class** — the standard Material
 * `WindowWidthSizeClass` (canonical 600/840dp breakpoints), so split-screen, freeform,
 * DeX and desktop windows all re-evaluate the same rule.
 *
 * Three tiers:
 *  - **Compact** (<600dp — phone portrait, folded cover, narrow split): single pane,
 *    **overlay** sessions drawer, no right panel.
 *  - **Medium** (600–839dp — foldable, tablet portrait, large-phone landscape): chat +
 *    persistent right info panel, with an **inline-push** rail closed by default.
 *  - **Expanded** (≥840dp — tablet landscape, unfolded foldable, desktop): the full
 *    **triptych** — rail open by default and persistent, alongside chat + right panel —
 *    except on a short (Compact-height) window (large phone in landscape), where the
 *    rail stays closed so three panes don't crowd a shallow viewport.
 */
internal fun chatLayoutFor(
    width: WindowWidthSizeClass,
    height: WindowHeightSizeClass,
): ChatLayout = when (chatPaneTier(width)) {
    ChatPaneTier.Compact -> ChatLayout(
        singlePane = true,
        showRightPanel = false,
        leftRailMode = LeftRailMode.Overlay,
        railPersistent = false,
    )
    ChatPaneTier.Medium -> ChatLayout(
        singlePane = false,
        showRightPanel = true,
        leftRailMode = LeftRailMode.InlinePush,
        railPersistent = false,
    )
    ChatPaneTier.Expanded -> ChatLayout(
        singlePane = false,
        showRightPanel = true,
        leftRailMode = LeftRailMode.InlinePush,
        // Persistent triptych only when there's vertical room; a short Expanded-width
        // window (large phone in landscape) keeps the rail closed by default.
        railPersistent = height != WindowHeightSizeClass.COMPACT,
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
    // The sessions rail is shared across chat destinations: scope its ViewModel to the
    // Activity (not the per-session nav entry) so switching sessions doesn't tear down and
    // reload the rail — otherwise the menu blanks out and repopulates on every switch.
    sessionListViewModel: SessionListViewModel =
        hiltViewModel(checkNotNull(LocalActivity.current) as ComponentActivity),
) {
    val sessionListState by sessionListViewModel.uiState.collectAsStateWithLifecycle()
    val chatUiState by chatViewModel.uiState.collectAsStateWithLifecycle()
    val chatCommands by chatViewModel.commands.collectAsStateWithLifecycle()

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
    // Inline-push rail visibility. Open by default at Expanded width (the persistent
    // triptych), closed otherwise; re-keyed on layout so a change (e.g. fold/unfold or a
    // rotation across a breakpoint) resets to the tier's default rather than reopening
    // with stale state. The effect closes the overlay drawer on the same change so it
    // never lingers when the layout switches out of overlay mode.
    var railOpen by remember(layout) { mutableStateOf(layout.railPersistent) }
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

    // onSelect runs when a session/new-session is chosen (it may keep a persistent rail
    // open); onCollapse runs only for the explicit collapse chevron (it always closes).
    val railPane: @Composable (Modifier, () -> Unit, () -> Unit) -> Unit = { mod, onSelect, onCollapse ->
        NavRailPane(
            uiState = sessionListState,
            activeSessionId = sessionId,
            activeDirectory = chatUiState.session?.directory,
            onSelectSession = { id -> onSelect(); onNavigateToSession(id) },
            onNewSession = {
                onSelect()
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
            onCollapse = onCollapse,
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
                    commands = chatCommands,
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
                        val closeDrawer = { scope.launch { drawerState.close() }; Unit }
                        railPane(Modifier.fillMaxSize(), closeDrawer, closeDrawer)
                    }
                },
            ) {
                centerPane(Modifier.fillMaxSize())
            }
            // Wider windows: the menu pushes the chat aside (inline). Open = the full
            // 220dp rail; collapsed = a narrow 60dp icon band (sessions + running status
            // stay reachable) rather than vanishing entirely.
            LeftRailMode.InlinePush -> Row(Modifier.fillMaxSize()) {
                if (railOpen) {
                    // Selecting a session keeps the persistent triptych rail open (only a
                    // manually-opened Medium rail collapses); the chat content swaps in
                    // place. The collapse chevron always closes the rail.
                    railPane(
                        Modifier.width(220.dp).fillMaxHeight(),
                        { if (!layout.railPersistent) railOpen = false },
                        { railOpen = false },
                    )
                } else {
                    CollapsedRail(
                        sessions = sessionListState.groups.flatMap { it.sessions },
                        statuses = sessionListState.statuses,
                        activeId = sessionId,
                        onExpand = { railOpen = true },
                        onNew = onNewSession,
                        onSelect = { id -> onNavigateToSession(id) },
                    )
                }
                Box(Modifier.width(1.dp).fillMaxHeight().background(Hairline))
                centerPane(Modifier.weight(1f))
            }
        }

        SnackbarHost(sessionSnackbar, Modifier.align(Alignment.BottomCenter))
    }
}

// ─── Collapsed icon rail (narrow band) ─────────────────────────────────────────

/**
 * The 60dp band the inline rail collapses to: an expand affordance, New, and the
 * sessions as initial-avatars so switching + running status stay reachable without
 * opening the full rail. Active = amber fill; a busy session shows a spinner badge.
 */
@Composable
private fun CollapsedRail(
    sessions: List<Session>,
    statuses: Map<String, String>,
    activeId: String,
    onExpand: () -> Unit,
    onNew: () -> Unit,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier.width(60.dp).fillMaxHeight().background(Surface),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Spacer(Modifier.height(8.dp))
        IconButton(onClick = onExpand, modifier = Modifier.size(40.dp)) {
            Icon(Icons.Default.Menu, contentDescription = "Expand navigation", tint = OnSurface, modifier = Modifier.size(20.dp))
        }
        IconButton(onClick = onNew, modifier = Modifier.size(40.dp)) {
            Icon(Icons.Default.Add, contentDescription = "New session", tint = Primary, modifier = Modifier.size(20.dp))
        }
        HorizontalDivider(color = Hairline, modifier = Modifier.width(28.dp).padding(vertical = 6.dp))
        Column(
            modifier = Modifier.weight(1f).verticalScroll(rememberScrollState()),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            sessions.forEach { s ->
                val active = s.id == activeId
                val busy = isSessionBusy(statuses[s.id])
                Box(contentAlignment = Alignment.Center) {
                    Box(
                        Modifier
                            .size(40.dp)
                            .clip(CircleShape)
                            .background(if (active) Secondary else SurfaceContainerHigh)
                            .clickable { onSelect(s.id) },
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(
                            text = sessionInitials(s.title),
                            fontSize = 13.sp,
                            fontWeight = FontWeight.SemiBold,
                            color = if (active) OnSecondary else OnSurfaceVariant,
                        )
                    }
                    if (busy) {
                        Spinner(
                            modifier = Modifier.align(Alignment.BottomEnd),
                            size = 13.dp,
                            color = if (active) OnSecondary else Secondary,
                        )
                    }
                }
            }
            Spacer(Modifier.height(8.dp))
        }
    }
}

private fun sessionInitials(title: String?): String {
    val t = title?.trim().orEmpty()
    if (t.isEmpty()) return "?"
    val words = t.split(Regex("\\s+")).filter { it.isNotEmpty() }
    return if (words.size >= 2) {
        "${words[0].first()}${words[1].first()}".uppercase()
    } else {
        t.take(2).uppercase()
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
    commands: List<CommandInfo> = emptyList(),
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
                    Box(Modifier.size(16.dp), contentAlignment = Alignment.Center) {
                        when (todo.status) {
                            "completed" -> Icon(
                                Icons.Default.Check,
                                contentDescription = null,
                                tint = Tertiary,
                                modifier = Modifier.size(15.dp),
                            )
                            "in_progress" -> Spinner(size = 13.dp, color = Secondary)
                            else -> Icon(
                                Icons.Default.RadioButtonUnchecked,
                                contentDescription = null,
                                tint = OnSurfaceFaint,
                                modifier = Modifier.size(12.dp),
                            )
                        }
                    }
                    Spacer(Modifier.width(8.dp))
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

        if (commands.isNotEmpty()) {
            InfoSectionHeader("COMMANDS")
            commands.forEach { cmd ->
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 1.dp),
                ) {
                    Text(
                        text = "/${cmd.name}",
                        fontFamily = Opcode42Mono,
                        fontSize = 12.5.sp,
                        color = LinkCyan,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.widthIn(max = 130.dp),
                    )
                    cmd.source?.takeIf { it == "mcp" || it == "skill" }?.let { src ->
                        Spacer(Modifier.width(6.dp))
                        Text(
                            text = src,
                            fontFamily = Opcode42Mono,
                            fontSize = 10.sp,
                            color = if (src == "mcp") HeaderPurple else OnSurfaceFaint,
                        )
                    }
                    cmd.description?.let { desc ->
                        Spacer(Modifier.width(8.dp))
                        Text(
                            text = desc,
                            fontSize = 12.sp,
                            color = OnSurfaceFaint,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                    }
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
