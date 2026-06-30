package dev.opcode42.app.ui

import androidx.activity.ComponentActivity
import androidx.activity.compose.LocalActivity
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.FormatListBulleted
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.RadioButtonUnchecked
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material3.*
import androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi
import androidx.compose.material3.adaptive.currentWindowAdaptiveInfo
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.layout
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.lerp
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
import dev.opcode42.core.design.rail.*
import dev.opcode42.core.design.theme.*
import dev.opcode42.feature.chat.ChatViewModel
import dev.opcode42.feature.chat.DRAFT_SESSION_ID
import dev.opcode42.feature.chat.TodoItem
import dev.opcode42.feature.chat.ui.*
import dev.opcode42.feature.sessions.SessionFilter
import dev.opcode42.feature.sessions.homeRelativeDir
import dev.opcode42.feature.sessions.SessionListEvent
import dev.opcode42.feature.sessions.SessionListUiState
import dev.opcode42.feature.sessions.SessionListViewModel
import dev.opcode42.feature.sessions.ui.SessionBrowser
import kotlin.math.roundToInt
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
    onOpenSettings: () -> Unit = {},
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

    // Session-list action errors (rename/archive/delete/reply/… from the rail) surface here — the
    // rail is the app's only session-list surface, so without this collector those events would
    // have no consumer. (Chat-side errors are handled by ChatScreen.)
    val sessionSnackbar = remember { SnackbarHostState() }
    LaunchedEffect(Unit) {
        sessionListViewModel.events.collect { event ->
            when (event) {
                is SessionListEvent.ShowError -> sessionSnackbar.showSnackbar(event.message)
            }
        }
    }
    // There is no full-screen session-list error surface anymore (the rail is the only session
    // list), so a catastrophic load failure is surfaced here as a snackbar instead.
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
    // The rail's open⇄collapsed morph progress (1f = open 220dp, 0f = collapsed 60dp). Declared
    // WITHOUT `by` so the host never recomposes per frame; the provider is read only inside the
    // draw/layout lambdas of the rail's children (see Modifier.railWidth, NavRailPane, SessionRow).
    val railProgress = animateFloatAsState(
        targetValue = if (railOpen) 1f else 0f,
        animationSpec = tween(durationMillis = 240, easing = FastOutSlowInEasing),
        label = "railProgress",
    )
    val railProgressProvider: () -> Float = { railProgress.value }

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
    // open); onCollapse runs only for the explicit collapse chevron (it always closes);
    // onExpand re-opens from the collapsed band; progress drives the open⇄collapsed morph.
    val railPane: @Composable (Modifier, () -> Unit, () -> Unit, () -> Unit, () -> Float) -> Unit =
        { mod, onSelect, onCollapse, onExpand, progress ->
            NavRailPane(
                uiState = sessionListState,
                activeSessionId = sessionId,
                activeDirectory = chatUiState.session?.directory,
                onSelectSession = { id -> onSelect(); onNavigateToSession(id) },
                onNewSession = {
                    onSelect()
                    onNewSession()
                },
                onOpenTasksBoard = onOpenTasksBoard,
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
                onExpand = onExpand,
                // Dismiss the menu first (closes the overlay drawer / collapses a non-persistent
                // rail) like onSelectSession/onNewSession, so Back from Settings doesn't reopen it.
                onOpenSettings = { onSelect(); onOpenSettings() },
                progress = progress,
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

    // The right context sidebar (shown when the info panel is on). Rendered as a slot
    // inside ChatScreen in multi-pane so the top bar spans the chat area above it.
    val infoSlot: (@Composable () -> Unit)? = if (rightPanelVisible) {
        {
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
    } else {
        null
    }

    // The chat surface. In multi-pane it hosts the [infoContent] sidebar so the top bar
    // spans the chat area (stream + sidebar); the rail stays outside (see InlinePush).
    val chat: @Composable (infoContent: (@Composable () -> Unit)?) -> Unit = { infoContent ->
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
            infoContent = infoContent,
            viewModel = chatViewModel,
        )
    }

    // The inline rail: a single pane that MORPHS between the open 220dp rail and the
    // collapsed 60dp icon band, width-driven by railProgress (no hard swap). Selecting a
    // session keeps a persistent (Expanded) rail open; only a manually-opened Medium rail
    // collapses. The collapse chevron closes; the expand chevron / collapsed search re-opens.
    val railSlot: @Composable () -> Unit = {
        railPane(
            Modifier.railWidth(railProgressProvider).fillMaxHeight(),
            { if (!layout.railPersistent) railOpen = false },
            { railOpen = false },
            { railOpen = true },
            railProgressProvider,
        )
    }

    Box(
        Modifier
            .fillMaxSize()
            .background(Surface)
            .systemBarsPadding(),
    ) {
        when (layout.leftRailMode) {
            // Compact width: single pane — the sessions menu floats over the chat as a
            // scrim drawer; the chat hosts no sidebar (rail = drawer).
            LeftRailMode.Overlay -> ModalNavigationDrawer(
                drawerState = drawerState,
                drawerContent = {
                    ModalDrawerSheet(Modifier.width(300.dp), drawerContainerColor = Surface) {
                        val closeDrawer = { scope.launch { drawerState.close() }; Unit }
                        // The overlay drawer always shows the full rail (no morph): progress = 1f.
                        railPane(Modifier.fillMaxSize(), closeDrawer, closeDrawer, {}, { 1f })
                    }
                },
            ) {
                chat(null)
            }
            // Wider windows: the sessions rail sits BESIDE the chat (outside it), so the
            // chat's top bar spans only the chat area (stream + sidebar) and stops at the
            // rail's edge — it does not extend left over the rail. Open = the full 220dp
            // rail; collapsed = the 60dp icon band.
            LeftRailMode.InlinePush -> Row(Modifier.fillMaxSize()) {
                railSlot()
                Box(Modifier.width(1.dp).fillMaxHeight().background(Hairline))
                Box(Modifier.weight(1f).fillMaxHeight()) {
                    chat(infoSlot)
                }
            }
        }

        SnackbarHost(sessionSnackbar, Modifier.align(Alignment.BottomCenter))
    }
}

// ─── Rail width morph ──────────────────────────────────────────────────────────

/**
 * Constrains the rail to `lerp(RailCollapsedWidth, RailOpenWidth, progress())`, read in the layout
 * phase so the per-frame morph never recomposes. The chat pane (a `weight(1f)` sibling) reflows smoothly.
 */
private fun Modifier.railWidth(progress: () -> Float): Modifier = layout { measurable, constraints ->
    val w = lerp(RailCollapsedWidth, RailOpenWidth, progress().coerceIn(0f, 1f)).roundToPx()
    val placeable = measurable.measure(constraints.copy(minWidth = w, maxWidth = w))
    layout(w, placeable.height) { placeable.place(0, 0) }
}

// ─── Left nav rail pane ────────────────────────────────────────────────────────

@Composable
internal fun NavRailPane(
    uiState: SessionListUiState,
    activeSessionId: String,
    activeDirectory: String?,
    onSelectSession: (String) -> Unit,
    onNewSession: () -> Unit,
    onOpenTasksBoard: () -> Unit,
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
    onExpand: () -> Unit,
    onOpenSettings: () -> Unit,
    progress: () -> Float,
    modifier: Modifier = Modifier,
) {
    // Flip the header's interactivity once at the midpoint (alpha=0 still hit-tests).
    val open by remember { derivedStateOf { progress() > 0.5f } }
    Column(modifier.fillMaxSize().background(SurfaceContainerLow)) {
        // Header: the wordmark + New fade out as the rail collapses, leaving a single chevron that
        // rotates 180° (collapse "‹" ⇄ expand "›") and slides from the open right edge to the
        // collapsed center.
        Box(Modifier.fillMaxWidth().height(54.dp)) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier
                    // Center the wordmark + New vertically in the 54dp header band so they sit on
                    // the chevron's baseline (which is center-aligned) rather than hugging the top.
                    .align(Alignment.CenterStart)
                    .fillMaxWidth()
                    // Front-load the fade (gone by progress≈0.4) so the wordmark + New never
                    // visibly squeeze as the rail narrows. End padding leaves room for the chevron.
                    .graphicsLayer { alpha = ((progress() - 0.4f) / 0.4f).coerceIn(0f, 1f) }
                    .padding(start = 14.dp, end = 50.dp),
            ) {
                Text(
                    text = "opcode42",
                    fontFamily = Opcode42Mono,
                    fontWeight = FontWeight.Bold,
                    fontSize = 16.sp,
                    letterSpacing = 1.sp,
                    color = OnSurface,
                    maxLines = 1,
                    modifier = Modifier.weight(1f),
                )
                // Subtle bordered "New" (design): surface-container-high fill, hairline border,
                // blue plus. Interactive only when the rail is open (+New is open-only).
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier
                        .clip(RoundedCornerShape(6.dp))
                        .background(SurfaceContainerHigh)
                        .border(BorderStroke(1.dp, Hairline), RoundedCornerShape(6.dp))
                        .then(if (open) Modifier.clickable(onClick = onNewSession) else Modifier)
                        .padding(horizontal = 9.dp, vertical = 6.dp),
                ) {
                    Icon(Icons.Default.Add, contentDescription = null, tint = Primary, modifier = Modifier.size(14.dp))
                    Spacer(Modifier.width(4.dp))
                    Text("New", fontSize = 12.5.sp, color = OnSurface)
                }
            }
            // One chevron, always present + interactive. Slides open-right (174dp) → collapsed-center
            // (10dp, so the 40dp box centers in the 60dp band) and rotates 180° so "‹" becomes "›".
            Box(
                Modifier
                    .align(Alignment.CenterStart)
                    .offset {
                        IntOffset(androidx.compose.ui.util.lerp(10.dp.toPx(), 174.dp.toPx(), progress()).roundToInt(), 0)
                    }
                    .size(40.dp)
                    .clickable { if (progress() > 0.5f) onCollapse() else onExpand() }
                    .graphicsLayer { rotationZ = (1f - progress()) * 180f },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    Icons.AutoMirrored.Filled.KeyboardArrowLeft,
                    contentDescription = if (open) "Collapse navigation" else "Expand navigation",
                    tint = OnSurfaceVariant,
                    modifier = Modifier.size(20.dp),
                )
            }
        }
        HorizontalDivider(color = Hairline, thickness = 1.dp)

        // Conversation / Tasks nav segment — Conversation is the current view (active);
        // Tasks opens the tasks board. Labels fade + icons glide to center as the rail collapses.
        // No horizontal padding here: NavRow spans the full band so the icon centers in 60dp.
        Column(
            modifier = Modifier.padding(vertical = 6.dp),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            NavRow(Icons.Outlined.ChatBubbleOutline, "Conversation", active = true, progress = progress, onClick = {})
            NavRow(Icons.AutoMirrored.Filled.FormatListBulleted, "Tasks", active = false, progress = progress, onClick = onOpenTasksBoard)
        }
        HorizontalDivider(color = Hairline, thickness = 1.dp, modifier = Modifier.padding(horizontal = 10.dp))

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
            containerColor = SurfaceContainerLow,
            progress = progress,
            onExpand = onExpand,
            modifier = Modifier.weight(1f),
        )

        // Persistent footer: a green "connected" dot + the `~`-relative workdir. Always shown
        // (even on a draft with no session yet) so the rail keeps its status line — falls back to
        // a bare "~" when there's no active directory.
        HorizontalDivider(color = Hairline, thickness = 1.dp)
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 10.dp, vertical = 8.dp),
        ) {
            Box(
                Modifier
                    // Open (progress 1): flush left (offset 0) so the path follows it. Collapsed
                    // (progress 0): offset 17dp → dot left edge ≈27, center 30 = the 60dp band's center.
                    .offset { IntOffset(androidx.compose.ui.util.lerp(17.dp.toPx(), 0f, progress()).roundToInt(), 0) }
                    .size(6.dp)
                    .clip(CircleShape)
                    .background(Tertiary),
            )
            Spacer(Modifier.width(6.dp))
            // `~`-relative display of the daemon-host path; fades out as the rail collapses.
            Text(
                text = homeRelativeDir(activeDirectory).ifEmpty { "~" },
                fontFamily = Opcode42Mono,
                fontSize = 11.sp,
                color = OnSurfaceFaint,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f).graphicsLayer { alpha = progress() },
            )
            // Settings is reached from here: the rail is the app's home surface (there's no
            // standalone session-list screen), so this gear is the path to Settings / Add-Server.
            // Fades with the rail and is tappable only when open (like the +New button).
            Icon(
                Icons.Default.Settings,
                contentDescription = "Settings",
                tint = OnSurfaceVariant,
                modifier = Modifier
                    .graphicsLayer { alpha = progress() }
                    .clip(CircleShape)
                    .then(if (open) Modifier.clickable(onClick = onOpenSettings) else Modifier)
                    .padding(5.dp)
                    .size(18.dp),
            )
        }
    }
}

/** A full-width Conversation / Tasks nav row. The icon is STATIC at the band-center inset (so it's
 *  already centered when the rail collapses to 60dp); only the label fades as [progress] 1f→0f. */
@Composable
private fun NavRow(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    label: String,
    active: Boolean,
    progress: () -> Float,
    onClick: () -> Unit,
) {
    val accent = Secondary
    val container = SecondaryContainer // hoisted: a @Composable token can't be read in a draw lambda
    Box(
        Modifier
            .fillMaxWidth()
            .height(40.dp)
            .clickable(onClick = onClick)
            // Active highlight — ONE shape that resizes from a full-width pill (open) into the
            // centered square (collapsed); see railActiveHighlight. No cross-fade of two boxes.
            .railActiveHighlight(active = active, progress = progress, container = container, accent = accent),
    ) {
        // Static at inset 22: a 16dp icon there spans 22–38, centered (30) in the 60dp band, so it
        // never moves as the rail retracts — only the label collapses away.
        Icon(
            icon,
            contentDescription = label,
            tint = if (active) accent else OnSurfaceVariant,
            modifier = Modifier
                .align(Alignment.CenterStart)
                .padding(start = 22.dp)
                .size(16.dp),
        )
        Text(
            text = label,
            fontSize = 13.5.sp,
            color = if (active) OnSurface else OnSurfaceVariant,
            fontWeight = if (active) FontWeight.Medium else FontWeight.Normal,
            maxLines = 1,
            softWrap = false,
            modifier = Modifier
                .align(Alignment.CenterStart)
                .padding(start = 44.dp)
                .graphicsLayer { alpha = progress() },
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
                    fontSize = 11.sp,
                    letterSpacing = 0.6.sp,
                    fontWeight = FontWeight.Bold,
                    color = HeaderPurple,
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
    // Purple uppercase sans — the design's single section-header voice across the rail,
    // the right info panel, and the phone list.
    Text(
        text = label,
        modifier = Modifier.padding(start = 10.dp, end = 10.dp, top = 9.dp, bottom = 3.dp),
        fontSize = 11.sp,
        letterSpacing = 0.6.sp,
        fontWeight = FontWeight.Bold,
        color = HeaderPurple,
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
