package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*
import dev.opcode42.core.design.brand.AsteriskMark
import dev.opcode42.core.design.brand.Spinner

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Archive
import androidx.compose.material.icons.filled.CallSplit
import androidx.compose.material.icons.filled.Compress
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.Terminal
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.input.nestedscroll.NestedScrollConnection
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.opcode42.core.model.Message
import dev.opcode42.core.model.ModelRef
import dev.opcode42.core.model.Part
import dev.opcode42.core.model.PatchPart
import dev.opcode42.core.model.SnapshotFileDiff
import dev.opcode42.core.store.OptimisticMessage
import dev.opcode42.feature.chat.ChatEvent
import dev.opcode42.feature.chat.ChatViewModel
import dev.opcode42.feature.chat.commands.ChatCommandActions
import dev.opcode42.feature.chat.commands.PaletteEntry
import dev.opcode42.feature.chat.commands.buildPaletteEntries
import dev.opcode42.feature.chat.commands.builtinCommands

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun ChatScreen(
    sessionId: String,
    onNavigateBack: () -> Unit,
    onOpenTerminal: (directory: String) -> Unit = {},
    onNavigateToSession: (sessionId: String) -> Unit = {},
    onNewSession: () -> Unit = {},
    onOpenTasksBoard: () -> Unit = {},
    applySystemInsets: Boolean = true,
    isMultiPane: Boolean = false,
    onOpenNavRail: () -> Unit = {},
    attentionBadge: Boolean = false,
    showInfoToggle: Boolean = false,
    infoPanelOpen: Boolean = true,
    onToggleInfoPanel: () -> Unit = {},
    showTodoSheet: Boolean = true,
    /** True for the lazy "new session" draft — no server session exists yet. */
    isDraft: Boolean = false,
    /**
     * Multi-pane sidebar slot: when [infoContent] is non-null the chat hosts the context
     * sidebar inside its own Scaffold content, so the top bar spans the chat area (stream
     * + sidebar) and the composer lives under the stream column only. The sessions rail
     * stays OUTSIDE the chat (beside it), so the top bar does not extend over the rail.
     */
    infoContent: (@Composable () -> Unit)? = null,
    viewModel: ChatViewModel = hiltViewModel(),
) {
    // On a draft, the first prompt creates the real session; navigate to it (the nav graph pushes
    // the chat on top of the home draft, so Back returns to the home page).
    LaunchedEffect(viewModel) {
        viewModel.navigateToSession.collect { newId -> onNavigateToSession(newId) }
    }

    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    val commands by viewModel.commands.collectAsStateWithLifecycle()
    val providers by viewModel.providers.collectAsStateWithLifecycle()
    val agents by viewModel.agents.collectAsStateWithLifecycle()
    val selectedModel by viewModel.selectedModel.collectAsStateWithLifecycle()
    val selectedAgent by viewModel.selectedAgent.collectAsStateWithLifecycle()
    val listState = rememberLazyListState()

    // Surface one-shot errors (a failed send/rename/share/…) as a snackbar.
    val snackbarHostState = remember { SnackbarHostState() }
    LaunchedEffect(Unit) {
        viewModel.events.collect { event ->
            when (event) {
                is ChatEvent.ShowError -> snackbarHostState.showSnackbar(event.message)
            }
        }
    }

    // ── Sticky-bottom auto-scroll ──────────────────────────────────────────────
    // The message list is reverseLayout: the newest message is index 0, anchored at the
    // bottom of the viewport. That gives requirements 1 and 2 almost for free:
    //  1. Entering a chat starts at index 0 — already at the bottom, no scroll.
    //  2. A streaming reply grows the bottom-anchored newest message upward, so the latest
    //     output stays pinned with no work; a *new* message is revealed by snapping back to
    //     index 0, but only while the user is still parked at the bottom.
    //  3. Scrolling up moves off index 0 and stops the snap; returning to the bottom resumes
    //     it. Only user drags/flings reach onPostScroll — our snap does not — so new content
    //     never overrides where the user scrolled to.
    val stickToBottom = remember(uiState.session?.id) { mutableStateOf(true) }
    val autoScrollConnection = remember(stickToBottom) {
        object : NestedScrollConnection {
            override fun onPostScroll(consumed: Offset, available: Offset, source: NestedScrollSource): Offset {
                stickToBottom.value =
                    listState.firstVisibleItemIndex == 0 && listState.firstVisibleItemScrollOffset == 0
                return Offset.Zero
            }
        }
    }
    // Key on stickToBottom (not listState): it is re-created — and reset to true — when the
    // session id changes (including the initial null → id transition), so the collector must
    // relaunch to read the live state object. Keying on the stable listState would freeze this
    // coroutine onto the first state instance, so scroll-up could never stop the snap.
    LaunchedEffect(stickToBottom) {
        snapshotFlow { uiState.messages.size + uiState.optimisticMessages.size }
            .collect { if (stickToBottom.value) listState.scrollToItem(0) }
    }

    // Show permission sheet if any are pending
    val pendingPermission = uiState.pendingPermissions.firstOrNull()
    val pendingQuestion = uiState.pendingQuestions.firstOrNull()

    val sessionDirectory = uiState.session?.directory
    var showInfoSheet by remember { mutableStateOf(false) }
    var showOverflow by remember { mutableStateOf(false) }
    var showModelPicker by remember { mutableStateOf(false) }
    var showRenameDialog by remember { mutableStateOf(false) }
    var showShareDialog by remember { mutableStateOf(false) }
    var showDeleteConfirm by remember { mutableStateOf(false) }
    var showArchiveConfirm by remember { mutableStateOf(false) }
    val clipboard = LocalClipboardManager.current

    // Capability surface for the `/` palette's built-in actions. Recreated each
    // recomposition so it always closes over the current session state/callbacks.
    val commandActions = object : ChatCommandActions {
        override val hasDirectory: Boolean get() = sessionDirectory != null
        override fun newSession() = onNewSession()
        override fun openSessions() = onOpenNavRail()
        override fun openModelPicker() { showModelPicker = true }
        override fun openTerminal() { sessionDirectory?.let { onOpenTerminal(it) } }
        override fun openInfo() { showInfoSheet = true }
        override fun renameSession() { showRenameDialog = true }
        override fun forkSession() = viewModel.forkSession { newId -> onNavigateToSession(newId) }
        override fun summarize() = viewModel.summarize()
        override fun shareSession() { showShareDialog = true }
        override fun archiveSession() { showArchiveConfirm = true }
        override fun deleteSession() { showDeleteConfirm = true }
    }
    // Rebuilt only when the inputs the list depends on change (daemon commands +
    // directory availability), not on every streaming recomposition.
    val paletteEntries = remember(commands, sessionDirectory) {
        buildPaletteEntries(builtinCommands, commands, commandActions)
    }

    // The strip shows the user's explicit pick if any, else the last-run state from the stream.
    val displayAgent = selectedAgent ?: uiState.agentMode
    val displayModelRef = selectedModel ?: run {
        val p = uiState.providerID
        val m = uiState.modelID
        if (p != null && m != null) ModelRef(providerID = p, modelID = m) else null
    }
    val displayModel = displayModelRef?.modelID
    val displayProvider = displayModelRef?.providerID

    // The composer (phone status strip + prompt input). Hosted in the Scaffold bottom
    // bar single-pane; in multi-pane it lives under the stream column so the top bar can
    // span the chat area (stream + sidebar) and the composer never runs under the sidebar.
    val composerBar: @Composable () -> Unit = {
        Column(
            Modifier
                .background(Surface)
                .windowInsetsPadding(WindowInsets.ime.union(WindowInsets.navigationBars)),
        ) {
            if (!isMultiPane) {
                HorizontalDivider(color = Hairline)
                StatusStrip(
                    mode = displayAgent,
                    model = displayModel,
                    provider = displayProvider,
                    tokens = uiState.session?.tokens,
                    onClick = if (providers.isNotEmpty() || agents.isNotEmpty()) {
                        { showModelPicker = true }
                    } else null,
                )
            }
            HorizontalDivider(color = Hairline)
            PromptInput(
                onSend = { text, attachments -> viewModel.sendPrompt(text, attachments) },
                enabled = pendingPermission == null && pendingQuestion == null,
                // isSending bridges the gap before the server flips status to "busy".
                busy = uiState.sessionStatus == "busy" || uiState.isSending,
                onStop = { viewModel.abort() },
                paletteEntries = paletteEntries,
                onSearchFiles = { query -> viewModel.searchFiles(query) },
                onPickEntry = { entry ->
                    when (entry) {
                        is PaletteEntry.Builtin -> entry.command.execute(commandActions)
                        is PaletteEntry.Daemon -> viewModel.runCommand(entry.name, "")
                    }
                },
            )
        }
    }

    Scaffold(
        containerColor = Surface,
        snackbarHost = { SnackbarHost(snackbarHostState) },
        topBar = {
            // Custom 52dp dense bar (design §1) — M3 TopAppBar's 64dp is too tall.
            Column(Modifier.background(Surface).then(if (applySystemInsets) Modifier.statusBarsPadding() else Modifier)) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(52.dp)
                        .padding(horizontal = 6.dp),
                ) {
                    // Leading icon opens the sessions menu on every form factor (overlay
                    // drawer on phones, inline rail on wider windows); system back returns
                    // to the home draft. A badge appears when a *background* session
                    // needs the user (pending permission/question).
                    IconButton(onClick = onOpenNavRail, modifier = Modifier.size(42.dp)) {
                        BadgedBox(
                            badge = { if (attentionBadge) Badge(containerColor = Secondary) },
                        ) {
                            Icon(
                                Icons.Default.Menu,
                                contentDescription = "Navigation menu",
                                tint = OnSurface,
                                modifier = Modifier.size(21.dp),
                            )
                        }
                    }
                    Column(modifier = Modifier.weight(1f).padding(horizontal = 4.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(
                                text = if (isDraft) "New session" else uiState.session?.title ?: "Session",
                                fontSize = 15.sp,
                                fontWeight = FontWeight.Medium,
                                color = OnSurface,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.weight(1f, fill = false),
                            )
                            Spinner(
                                visible = uiState.sessionStatus == "busy",
                                modifier = Modifier.padding(start = 8.dp),
                                color = Secondary,
                            )
                        }
                        uiState.session?.directory?.let { dir ->
                            HeaderSubtitle(directory = dir, branch = uiState.branch)
                        }
                    }
                    if (!isDraft && isMultiPane) {
                        // Mode badge + model — tap opens the model/agent picker (the
                        // multi-pane equivalent of the phone status strip's tap target).
                        val openPicker: (() -> Unit)? =
                            if (providers.isNotEmpty() || agents.isNotEmpty()) {
                                { showModelPicker = true }
                            } else {
                                null
                            }
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            modifier = (
                                if (openPicker != null) {
                                    Modifier.clip(Opcode42Shapes.sm).clickable(onClick = openPicker)
                                } else {
                                    Modifier
                                }
                                ).padding(horizontal = 2.dp),
                        ) {
                            Text(
                                text = (displayAgent ?: "build").replaceFirstChar { it.uppercase() },
                                fontFamily = Opcode42Mono,
                                fontSize = 11.sp,
                                fontWeight = FontWeight.Bold,
                                color = OnPrimary,
                                modifier = Modifier
                                    .clip(Opcode42Shapes.xs)
                                    .background(Primary)
                                    .padding(horizontal = 6.dp, vertical = 2.dp),
                            )
                            if (displayModel != null) {
                                Spacer(Modifier.width(6.dp))
                                Text(
                                    text = displayModel,
                                    fontFamily = Opcode42Mono,
                                    fontSize = 11.sp,
                                    color = OnSurfaceVariant,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.widthIn(max = 100.dp),
                                )
                            }
                        }
                        if (showInfoToggle) {
                            // Collapse/expand the right session-info panel (on by default).
                            IconButton(onClick = onToggleInfoPanel, modifier = Modifier.size(42.dp)) {
                                Icon(
                                    Icons.Default.Info,
                                    contentDescription = if (infoPanelOpen) "Hide session info" else "Show session info",
                                    tint = if (infoPanelOpen) Primary else OnSurfaceVariant,
                                    modifier = Modifier.size(20.dp),
                                )
                            }
                        } else {
                            Spacer(Modifier.width(2.dp))
                        }
                    } else if (!isDraft) {
                        IconButton(onClick = { showInfoSheet = true }, modifier = Modifier.size(42.dp)) {
                            Icon(Icons.Default.Info, contentDescription = "Session info", tint = OnSurfaceVariant, modifier = Modifier.size(20.dp))
                        }
                    }
                    // No overflow actions on a draft — there's no session to rename/fork/share/delete yet.
                    if (!isDraft) Box {
                        IconButton(onClick = { showOverflow = true }, modifier = Modifier.size(42.dp)) {
                            Icon(Icons.Default.MoreVert, contentDescription = "More", tint = OnSurfaceVariant, modifier = Modifier.size(20.dp))
                        }
                        OverflowMenu(
                            expanded = showOverflow,
                            onDismiss = { showOverflow = false },
                            isShared = uiState.session?.share != null,
                            onTerminal = sessionDirectory?.let { dir ->
                                { showOverflow = false; onOpenTerminal(dir) }
                            },
                            onRename = {
                                showOverflow = false
                                showRenameDialog = true
                            },
                            onFork = {
                                showOverflow = false
                                viewModel.forkSession { newId -> onNavigateToSession(newId) }
                            },
                            onSummarize = {
                                showOverflow = false
                                viewModel.summarize()
                            },
                            onShare = {
                                showOverflow = false
                                showShareDialog = true
                            },
                            onArchive = {
                                showOverflow = false
                                showArchiveConfirm = true
                            },
                            onDelete = {
                                showOverflow = false
                                showDeleteConfirm = true
                            },
                        )
                    }
                }
                HorizontalDivider(color = Hairline, thickness = 1.dp)
            }
        },
        // Single-pane: the composer is the Scaffold bottom bar. Multi-pane: it lives under
        // the stream column (see content below) so it never runs under the sidebar — and,
        // keyed on isMultiPane (not infoContent), it never re-parents when the info panel
        // is toggled, which would otherwise drop the in-progress prompt.
        bottomBar = { if (!isMultiPane) composerBar() },
    ) { padding ->
        Row(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            // Multi-pane: (stream + composer) · sidebar, UNDER a top bar that spans the
            // chat area only (the rail lives outside, beside the chat, so the bar stops at
            // the rail's edge). Single-pane: this Row holds just the stream column.
            Column(Modifier.weight(1f).fillMaxHeight()) {
                Box(Modifier.weight(1f).fillMaxWidth()) {
                    LazyColumn(
                        state = listState,
                        reverseLayout = true, // newest at the bottom (index 0); see sticky-scroll note above
                        contentPadding = PaddingValues(top = 6.dp, bottom = 64.dp), // 6+8 ≈ 14dp top gutter; clear the sheet peek
                        modifier = Modifier
                            .fillMaxHeight()
                            .fillMaxWidth()
                            .widthIn(max = 720.dp) // tablet: cap + center the stream
                            .align(Alignment.TopCenter)
                            .nestedScroll(autoScrollConnection),
                        // NOTE: no imeNestedScroll() — on a reverseLayout list it applies an
                        // inverted IME offset for one frame on every scroll, so the stream visibly
                        // jumps and snaps back as you scroll. The composer's own
                        // windowInsetsPadding(WindowInsets.ime…) keeps it above the keyboard — it's
                        // the Scaffold bottom bar single-pane, an in-content sibling multi-pane —
                        // so the list just needs to scroll normally.
                    ) {
                        // reverseLayout: emit newest-first so the freshest content is index 0 (bottom).
                        // Optimistic (just-sent, unconfirmed) messages are the newest, then the server
                        // messages newest→oldest above them.
                        items(uiState.optimisticMessages.asReversed(), key = { "opt:${it.id}" }) { opt ->
                            OptimisticMessageBlock(opt)
                        }
                        items(uiState.messages.asReversed(), key = { it.id }) { message ->
                            // SSE live parts supersede REST-loaded parts when present, but
                            // PatchParts from SSE may lack the `files` list that the REST
                            // endpoint includes — fall back to the REST-loaded part in that case.
                            val liveParts = uiState.parts[message.id]
                            val effectiveParts: List<Part> = remember(liveParts, message.parts) {
                                if (liveParts == null) {
                                    message.parts
                                } else {
                                    val byId = message.parts.associateBy { it.id }
                                    liveParts.map { lp ->
                                        if (lp is PatchPart && lp.files.isEmpty()) byId[lp.id] ?: lp
                                        else lp
                                    }
                                }
                            }
                            MessageBlock(
                                message = message,
                                parts = effectiveParts,
                                diffs = uiState.diffs,
                            )
                        }
                    }

                    // Initial message load: entering a session before anything has streamed in.
                    if (uiState.isLoading && uiState.messages.isEmpty() && uiState.optimisticMessages.isEmpty()) {
                        Spinner(
                            modifier = Modifier.align(Alignment.Center),
                            color = Secondary,
                        )
                    }

                    // New-session splash: the dual-arc mark + prompt until the first message lands.
                    if (isDraft && !uiState.isLoading && uiState.messages.isEmpty() && uiState.optimisticMessages.isEmpty()) {
                        Column(
                            horizontalAlignment = Alignment.CenterHorizontally,
                            modifier = Modifier.align(Alignment.Center).padding(24.dp),
                        ) {
                            AsteriskMark(size = 132.dp, color = OnSurface)
                            Spacer(Modifier.height(22.dp))
                            Text(
                                text = "What should we build?",
                                fontSize = 14.sp,
                                color = OnSurfaceVariant,
                            )
                        }
                    }

                    // Todos dock — only in single/medium pane; moves to info panel in expanded.
                    if (showTodoSheet) {
                        TodoSheet(
                            todos = uiState.todos,
                            onOpenTasksBoard = onOpenTasksBoard,
                        )
                    }
                }
                // Multi-pane → composer sits under the stream column (not the bottom bar)
                // so it never runs under the sidebar. Keyed on isMultiPane (not infoContent)
                // so toggling the info panel doesn't re-parent + reset the composer.
                if (isMultiPane) composerBar()
            }
            infoContent?.let {
                Box(Modifier.width(1.dp).fillMaxHeight().background(Hairline))
                it()
            }
        }

        // A8 — Permission sheet (non-dismissible)
        pendingPermission?.let { req ->
            PermissionSheet(
                permission = req,
                onApprove = { viewModel.replyPermission(req.id, allow = true) },
                onDeny = { viewModel.replyPermission(req.id, allow = false) },
            )
        }

        // A8 — Question sheet (non-dismissible)
        pendingQuestion?.let { req ->
            QuestionSheet(
                question = req,
                onReply = { answer -> viewModel.replyQuestion(req.id, answer) },
                onReject = { viewModel.rejectQuestion(req.id) },
            )
        }

        if (showInfoSheet) {
            uiState.session?.let { session ->
                SessionInfoSheet(session = session, onDismiss = { showInfoSheet = false })
            }
        }

        if (showRenameDialog) {
            RenameSessionDialog(
                current = uiState.session?.title,
                onConfirm = { title ->
                    viewModel.renameSession(title)
                    showRenameDialog = false
                },
                onDismiss = { showRenameDialog = false },
            )
        }

        if (showShareDialog) {
            ShareSessionDialog(
                url = uiState.session?.share?.url,
                onShare = { viewModel.shareSession() },
                onUnshare = {
                    viewModel.unshareSession()
                    showShareDialog = false
                },
                onCopy = { url -> clipboard.setText(AnnotatedString(url)) },
                onDismiss = { showShareDialog = false },
            )
        }

        if (showArchiveConfirm) {
            ConfirmActionDialog(
                title = "Archive session",
                message = "Archive this session? It will be removed from the active list.",
                confirmLabel = "Archive",
                onConfirm = {
                    showArchiveConfirm = false
                    viewModel.archiveSession { onNavigateBack() }
                },
                onDismiss = { showArchiveConfirm = false },
            )
        }

        if (showDeleteConfirm) {
            ConfirmActionDialog(
                title = "Delete session",
                message = "Delete this session permanently? This can't be undone.",
                confirmLabel = "Delete",
                destructive = true,
                onConfirm = {
                    showDeleteConfirm = false
                    viewModel.deleteSession { onNavigateBack() }
                },
                onDismiss = { showDeleteConfirm = false },
            )
        }

        if (showModelPicker) {
            ModelPickerSheet(
                providers = providers,
                agents = agents,
                selectedModel = displayModelRef,
                selectedAgent = displayAgent,
                onSelectModel = { ref ->
                    viewModel.selectModel(ref)
                    showModelPicker = false
                },
                onSelectAgent = { name ->
                    viewModel.selectAgent(name)
                    showModelPicker = false
                },
                onDismiss = { showModelPicker = false },
            )
        }
    }
}

@Composable
private fun OverflowMenu(
    expanded: Boolean,
    onDismiss: () -> Unit,
    isShared: Boolean,
    onTerminal: (() -> Unit)? = null,
    onRename: () -> Unit,
    onFork: () -> Unit,
    onSummarize: () -> Unit,
    onShare: () -> Unit,
    onArchive: () -> Unit,
    onDelete: () -> Unit,
) {
    DropdownMenu(
        expanded = expanded,
        onDismissRequest = onDismiss,
        containerColor = SurfaceContainerHigh,
    ) {
        onTerminal?.let { open ->
            DropdownMenuItem(
                text = { Text("Open terminal", color = OnSurface) },
                leadingIcon = { Icon(Icons.Default.Terminal, contentDescription = null, tint = OnSurfaceVariant) },
                onClick = open,
            )
            HorizontalDivider(color = Hairline)
        }
        DropdownMenuItem(
            text = { Text("Rename session", color = OnSurface) },
            leadingIcon = { Icon(Icons.Default.Edit, contentDescription = null, tint = OnSurfaceVariant) },
            onClick = onRename,
        )
        DropdownMenuItem(
            text = { Text("Fork session", color = OnSurface) },
            leadingIcon = { Icon(Icons.Default.CallSplit, contentDescription = null, tint = OnSurfaceVariant) },
            onClick = onFork,
        )
        DropdownMenuItem(
            text = { Text("Summarize context", color = OnSurface) },
            leadingIcon = { Icon(Icons.Default.Compress, contentDescription = null, tint = OnSurfaceVariant) },
            onClick = onSummarize,
        )
        DropdownMenuItem(
            text = { Text(if (isShared) "Sharing… (manage)" else "Share session", color = OnSurface) },
            leadingIcon = { Icon(Icons.Default.Share, contentDescription = null, tint = OnSurfaceVariant) },
            onClick = onShare,
        )
        HorizontalDivider(color = Hairline)
        DropdownMenuItem(
            text = { Text("Archive session", color = OnSurface) },
            leadingIcon = { Icon(Icons.Default.Archive, contentDescription = null, tint = OnSurfaceVariant) },
            onClick = onArchive,
        )
        DropdownMenuItem(
            text = { Text("Delete session", color = Error) },
            leadingIcon = { Icon(Icons.Default.Delete, contentDescription = null, tint = Error) },
            onClick = onDelete,
        )
    }
}

@Composable
private fun MessageBlock(
    message: Message,
    parts: List<Part>,
    diffs: Map<String, List<SnapshotFileDiff>> = emptyMap(),
) {
    Column(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
        if (message.role == "user") {
            UserMessageBlock(parts, diffs)
        } else {
            if (message.isSummary) CompactionMarker()
            AssistantMessageBlock(parts, diffs)
        }
    }
}

/** Labeled hairline marker shown above a context-compaction summary message. */
@Composable
private fun CompactionMarker() {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 6.dp),
    ) {
        HorizontalDivider(color = Hairline, modifier = Modifier.weight(1f))
        Text(
            text = "context summarized",
            fontFamily = Opcode42Mono,
            fontSize = 11.sp,
            color = HeaderPurple,
            modifier = Modifier.padding(horizontal = 10.dp),
        )
        HorizontalDivider(color = Hairline, modifier = Modifier.weight(1f))
    }
}

/** Renders a message's parts, grouping consecutive tool calls into one card. */
@Composable
private fun StreamParts(parts: List<Part>, diffs: Map<String, List<SnapshotFileDiff>>) {
    val items = remember(parts) { groupRenderItems(parts) }
    items.forEach { item ->
        when (item) {
            is RenderItem.Tools -> ToolRowGroup(item.parts)
            is RenderItem.Single -> PartRenderer(item.part, diffs = diffs)
            is RenderItem.Patch -> PartRenderer(item.part, editParts = item.editParts, diffs = diffs)
        }
    }
}

@Composable
private fun UserMessageBlock(
    parts: List<Part>,
    diffs: Map<String, List<SnapshotFileDiff>> = emptyMap(),
) {
    // 2dp primary left accent rail drawn relative to the measured height.
    val rail = Primary
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .drawBehind { drawRect(rail, size = Size(2.dp.toPx(), size.height)) }
            .padding(start = 13.dp, end = 14.dp),
    ) {
        StreamParts(parts, diffs)
    }
}

@Composable
private fun AssistantMessageBlock(
    parts: List<Part>,
    diffs: Map<String, List<SnapshotFileDiff>> = emptyMap(),
) {
    Column(modifier = Modifier.padding(horizontal = 0.dp)) {
        StreamParts(parts, diffs)
    }
}

@Composable
private fun OptimisticMessageBlock(opt: OptimisticMessage) {
    val rail = Primary
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 8.dp)
            .drawBehind { drawRect(rail, size = Size(2.dp.toPx(), size.height)) }
            .padding(start = 13.dp, end = 14.dp),
    ) {
        Text(
            text = opt.text,
            style = MaterialTheme.typography.bodyMedium.copy(fontSize = 14.5.sp),
            color = OnSurface.copy(alpha = 0.6f),
        )
        LinearProgressIndicator(
            modifier = Modifier.fillMaxWidth().padding(top = 4.dp).height(1.dp),
            color = Primary,
            trackColor = Hairline,
        )
    }
}
