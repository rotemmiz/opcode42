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
import androidx.compose.material.icons.filled.Checklist
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.Difference
import androidx.compose.material3.*
import androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi
import androidx.compose.material3.adaptive.currentWindowAdaptiveInfo
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.layout
import androidx.compose.ui.text.TextStyle
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
import dev.opcode42.core.design.text.StartEllipsisText
import dev.opcode42.core.design.theme.*
import dev.opcode42.feature.chat.ChatViewModel
import dev.opcode42.feature.chat.DRAFT_SESSION_ID
import dev.opcode42.feature.chat.TodoItem
import dev.opcode42.feature.chat.ui.*
import dev.opcode42.feature.sessions.SessionFilter
import dev.opcode42.feature.sessions.relativeTime
import dev.opcode42.core.design.text.homeRelativeDir
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

    val aggregatedDiffs = remember(
        chatUiState.messages,
        chatUiState.parts,
        chatUiState.diffs,
        chatUiState.session?.directory,
    ) {
        val dir = chatUiState.session?.directory
        // Mirror what the conversation shows: real snapshot diffs where loaded, else the
        // synthetic diff rebuilt from edit-tool inputs — so an edit that's already visible
        // in the stream (snapshot diff not yet/ever provided) still lands in CHANGES.
        sessionFileDiffs(chatUiState.messages, chatUiState.parts, chatUiState.diffs)
            // Relativize to the session's working dir first: edit/VCS paths carry no
            // relativity guarantee, so an absolute path would otherwise truncate to its
            // (useless) prefix under end-ellipsis — and relativizing also merges any
            // absolute/relative duplicates of the same file before grouping.
            .map { it.copy(file = relativeToDir(it.file, dir)) }
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
                messageCount = chatUiState.messages.size,
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
            // `~`-relative daemon-host path; start-ellipsized so the leaf dir survives a narrow
            // rail (…/git/opcode42). Fades out as the rail collapses.
            StartEllipsisText(
                text = homeRelativeDir(activeDirectory).ifEmpty { "~" },
                style = TextStyle(fontFamily = Opcode42Mono, fontSize = 11.sp, color = OnSurfaceFaint),
                modifier = Modifier
                    .weight(1f)
                    .graphicsLayer { alpha = progress() },
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
    messageCount: Int = 0,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier
            .fillMaxSize()
            .background(Surface)
            .verticalScroll(rememberScrollState()),
    ) {
        // The model + agent-mode are also shown in the chat top bar; the panel repeats them
        // here (the design's MODEL block) and starts straight at SESSION — no panel header.
        if (session != null) {
            SbSection("SESSION") {
                KV("title", session.title ?: "Untitled", mono = false)
                KV("id", session.id.take(8))
                val created = session.time?.created ?: 0L
                if (created > 0L) {
                    KV("started", startedLabel(created, messageCount))
                }
            }
        }

        if (modelID != null || providerID != null) {
            SbSection("MODEL") {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier.fillMaxWidth().heightIn(min = 28.dp),
                ) {
                    ModeChip(agentMode ?: "build")
                    if (modelID != null) {
                        Spacer(Modifier.width(8.dp))
                        Text(
                            text = modelID,
                            fontSize = 13.sp,
                            fontWeight = FontWeight.Medium,
                            color = OnSurface,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
                providerID?.let { KV("provider", it.replaceFirstChar { c -> c.uppercase() }) }
            }
        }

        if (tokens != null) {
            val used = tokens.contextFootprint
            val fraction = (used.toFloat() / 200_000L).coerceIn(0f, 1f)
            SbSection("CONTEXT") {
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Row(Modifier.weight(1f)) {
                        Text(formatTokens(used), fontFamily = Opcode42Mono, fontSize = 12.5.sp, color = OnSurface)
                        Text(" / 200K", fontFamily = Opcode42Mono, fontSize = 12.5.sp, color = OnSurfaceFaint)
                    }
                    Text(
                        text = "${(fraction * 100).toInt()}%",
                        fontFamily = Opcode42Mono,
                        fontSize = 12.5.sp,
                        color = Primary,
                    )
                }
                Box(
                    Modifier
                        .fillMaxWidth()
                        .padding(top = 8.dp)
                        .height(6.dp)
                        .clip(RoundedCornerShape(3.dp))
                        .background(SurfaceContainerHighest),
                ) {
                    Box(Modifier.fillMaxHeight().fillMaxWidth(fraction).background(Primary))
                }
                val cost = session?.cost
                if (cost != null && cost > 0.0) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier.padding(top = 9.dp),
                    ) {
                        Text("$", fontFamily = Opcode42Mono, fontSize = 12.sp, color = OnSurfaceFaint)
                        Spacer(Modifier.width(5.dp))
                        Text("%.2f".format(cost), fontFamily = Opcode42Mono, fontSize = 12.sp, color = OnSurfaceVariant)
                        Spacer(Modifier.width(5.dp))
                        Text("this session", fontFamily = Opcode42Mono, fontSize = 12.sp, color = OnSurfaceFaint)
                    }
                }
            }
        }

        if (diffs.isNotEmpty()) {
            SbSection(
                title = "CHANGES",
                count = "${diffs.size} ${if (diffs.size == 1) "file" else "files"}",
                action = {
                    Icon(Icons.Outlined.Difference, contentDescription = null, tint = LinkCyan, modifier = Modifier.size(14.dp))
                },
            ) {
                // Hoist the amber accent out of the per-row draw scope (it's a @Composable token).
                val accent = Secondary
                diffs.forEachIndexed { index, diff ->
                    // Accent the most-changed file (first — diffs are churn-sorted) with the
                    // amber active-row treatment shared with the sessions rail. Only when it
                    // stands out: skip a lone row, and a 0/0 (status-only) top row.
                    val active = index == 0 && diffs.size > 1 && (diff.additions + diff.deletions) > 0
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(4.dp))
                            .then(
                                if (active) {
                                    Modifier
                                        .background(SecondaryContainer)
                                        .drawBehind {
                                            drawRect(accent, size = Size(2.5.dp.toPx(), size.height))
                                        }
                                } else {
                                    Modifier
                                },
                            )
                            .padding(vertical = 2.dp),
                    ) {
                        Icon(
                            Icons.AutoMirrored.Outlined.InsertDriveFile,
                            contentDescription = null,
                            tint = if (active) Secondary else OnSurfaceFaint,
                            modifier = Modifier.size(13.dp),
                        )
                        Spacer(Modifier.width(6.dp))
                        // Pin the filename and let only the directory prefix ellipsize: this
                        // Compose BOM has no start/middle ellipsis, so a single full-path Text
                        // would clip the filename (the useful tail) on a deep path.
                        val path = diff.file?.takeIf { it.isNotBlank() } ?: "unknown"
                        val cut = path.lastIndexOf('/')
                        val pathColor = if (active) Secondary else Tertiary
                        Row(
                            modifier = Modifier.weight(1f),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            if (cut >= 0) {
                                Text(
                                    text = path.substring(0, cut + 1),
                                    fontFamily = Opcode42Mono,
                                    fontSize = 12.5.sp,
                                    color = pathColor,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.weight(1f, fill = false),
                                )
                            }
                            Text(
                                text = path.substring(cut + 1),
                                fontFamily = Opcode42Mono,
                                fontSize = 12.5.sp,
                                color = pathColor,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                        Spacer(Modifier.width(6.dp))
                        // Always show both counts; a zero stays dim rather than vanishing,
                        // so the +adds / −dels columns line up down the list.
                        Text(
                            text = "+${diff.additions}",
                            fontFamily = Opcode42Mono,
                            fontSize = 12.5.sp,
                            color = if (diff.additions > 0) Tertiary else OnSurfaceFaint,
                        )
                        Spacer(Modifier.width(4.dp))
                        Text(
                            text = "−${diff.deletions}",
                            fontFamily = Opcode42Mono,
                            fontSize = 12.5.sp,
                            color = if (diff.deletions > 0) Error else OnSurfaceFaint,
                        )
                    }
                }
            }
        }

        if (todos.isNotEmpty()) {
            val activeCount = todos.count { it.status == "in_progress" }
            SbSection(
                title = "TODOS",
                count = if (activeCount > 0) "$activeCount active" else null,
                action = {
                    Icon(Icons.Filled.Checklist, contentDescription = null, tint = LinkCyan, modifier = Modifier.size(14.dp))
                },
            ) {
                todos.forEach { todo -> TodoRow(todo) }
            }
        }

        if (commands.isNotEmpty()) {
            SbSection("COMMANDS") {
                commands.forEach { cmd -> CommandRow(cmd) }
            }
        }

        Spacer(Modifier.height(8.dp))
    }
}

/**
 * A bordered section block — the design's `SbSection`: a purple uppercase kicker, an
 * optional mono count, an optional trailing cyan action glyph, then content, closed by a
 * full-bleed hairline divider.
 */
@Composable
private fun SbSection(
    title: String,
    count: String? = null,
    action: (@Composable () -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column {
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp)) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.fillMaxWidth().padding(bottom = 10.dp),
            ) {
                Text(
                    text = title,
                    fontSize = 11.sp,
                    letterSpacing = 0.6.sp,
                    fontWeight = FontWeight.Bold,
                    color = HeaderPurple,
                )
                if (count != null) {
                    Text(
                        text = count,
                        fontFamily = Opcode42Mono,
                        fontSize = 10.5.sp,
                        color = OnSurfaceFaint,
                        modifier = Modifier.padding(start = 7.dp),
                    )
                }
                if (action != null) {
                    Spacer(Modifier.weight(1f))
                    action()
                }
            }
            content()
        }
        HorizontalDivider(color = Hairline, thickness = 1.dp)
    }
}

/** Key→value row (design `KV`): sans key in a fixed gutter, mono-by-default value. */
@Composable
private fun KV(key: String, value: String, mono: Boolean = true) {
    Row(
        modifier = Modifier.fillMaxWidth().heightIn(min = 24.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text = key, fontSize = 12.sp, color = OnSurfaceFaint, modifier = Modifier.width(64.dp))
        Text(
            text = value,
            fontFamily = if (mono) Opcode42Mono else null,
            fontSize = 13.sp,
            color = OnSurface,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
    }
}

/** The agent-mode pill ("Build") — the same chip the chat top bar uses. */
@Composable
private fun ModeChip(mode: String) {
    Text(
        text = mode.replaceFirstChar { it.uppercase() },
        fontFamily = Opcode42Mono,
        fontSize = 11.sp,
        fontWeight = FontWeight.Bold,
        color = OnPrimary,
        modifier = Modifier
            .clip(Opcode42Shapes.xs)
            .background(Primary)
            .padding(horizontal = 6.dp, vertical = 2.dp),
    )
}

/** A todo row (design `TodoRow`): status marker, text, and the chase loader while in progress. */
@Composable
private fun TodoRow(todo: TodoItem) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier.fillMaxWidth().heightIn(min = 30.dp),
    ) {
        TodoMarker(todo.status)
        Spacer(Modifier.width(10.dp))
        Text(
            text = todo.content,
            fontSize = 13.sp,
            lineHeight = 17.sp,
            fontWeight = if (todo.status == "in_progress") FontWeight.SemiBold else FontWeight.Normal,
            color = when (todo.status) {
                "in_progress" -> Secondary
                "completed" -> OnSurfaceVariant
                else -> OnSurface
            },
            modifier = Modifier.weight(1f),
        )
        // The brand chase loader, top-aligned; fades in/out with the in-progress state.
        Spinner(
            visible = todo.status == "in_progress",
            modifier = Modifier.align(Alignment.Top).padding(start = 8.dp),
            color = Secondary,
        )
    }
}

@Composable
private fun TodoMarker(status: String) {
    when (status) {
        "completed" -> Box(
            Modifier.size(16.dp).clip(CircleShape).background(Tertiary),
            contentAlignment = Alignment.Center,
        ) {
            Icon(Icons.Filled.Check, contentDescription = null, tint = OnPrimary, modifier = Modifier.size(11.dp))
        }
        "in_progress" -> Box(
            Modifier.size(16.dp).clip(CircleShape).border(2.dp, Secondary, CircleShape),
            contentAlignment = Alignment.Center,
        ) {
            Box(Modifier.size(6.dp).clip(CircleShape).background(Secondary))
        }
        else -> Box(
            Modifier
                .size(16.dp)
                .clip(RoundedCornerShape(4.dp))
                .border(2.dp, OnSurfaceGhost, RoundedCornerShape(4.dp)),
        )
    }
}

/** A slash-command row (design `Commands`): cyan name, optional source badge, description. */
@Composable
private fun CommandRow(cmd: CommandInfo) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier.fillMaxWidth().heightIn(min = 24.dp),
    ) {
        Text(
            text = "/${cmd.name}",
            fontFamily = Opcode42Mono,
            fontSize = 12.5.sp,
            color = LinkCyan,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.widthIn(min = 78.dp, max = 130.dp),
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
                fontSize = 12.5.sp,
                color = OnSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
        }
    }
}

/** "9 min ago · 14 msgs" — the session's start age (reusing [relativeTime] past a week). */
private fun startedLabel(createdMs: Long, msgs: Int): String {
    val ago = verboseAgo(createdMs)
    val msgPart = if (msgs > 0) "$msgs ${if (msgs == 1) "msg" else "msgs"}" else ""
    return listOf(ago, msgPart).filter { it.isNotEmpty() }.joinToString(" · ")
}

private fun verboseAgo(epochMs: Long, now: Long = System.currentTimeMillis()): String {
    if (epochMs <= 0L) return ""
    val diff = now - epochMs
    val mins = diff / 60_000
    val hours = diff / 3_600_000
    val days = diff / 86_400_000
    return when {
        diff < 60_000 -> "just now"
        mins < 60 -> "$mins min ago"
        hours < 24 -> "$hours ${if (hours == 1L) "hour" else "hours"} ago"
        days < 7 -> "$days ${if (days == 1L) "day" else "days"} ago"
        else -> relativeTime(epochMs)
    }
}

/** Collapse a diff path to be relative to the session's working dir (a no-op if it
 *  already is, or if the dir is unknown), so the filename — not a long absolute prefix —
 *  is what survives in the narrow CHANGES list. */
private fun relativeToDir(file: String?, dir: String?): String? {
    if (file == null || dir.isNullOrEmpty()) return file
    // Match on the path boundary so a sibling sharing a prefix (dir `/a/project`,
    // file `/a/projectX/y`) isn't mistaken for a child.
    val base = dir.removeSuffix("/")
    return if (file.startsWith("$base/")) file.removePrefix("$base/") else file
}

private fun formatTokens(n: Long): String = when {
    n >= 1_000_000 -> "%.1fM".format(n / 1_000_000.0)
    n >= 1_000 -> "%.1fK".format(n / 1_000.0)
    else -> n.toString()
}
