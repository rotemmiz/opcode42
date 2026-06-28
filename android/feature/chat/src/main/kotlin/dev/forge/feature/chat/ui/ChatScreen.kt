package dev.forge.feature.chat.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Archive
import androidx.compose.material.icons.filled.CallSplit
import androidx.compose.material.icons.filled.Compress
import androidx.compose.material.icons.filled.DarkMode
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.LightMode
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
import dev.forge.core.model.Message
import dev.forge.core.model.ModelRef
import dev.forge.core.model.Part
import dev.forge.core.model.PatchPart
import dev.forge.core.model.SnapshotFileDiff
import dev.forge.core.store.OptimisticMessage
import dev.forge.feature.chat.ChatViewModel

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun ChatScreen(
    sessionId: String,
    onNavigateBack: () -> Unit,
    onOpenTerminal: (directory: String) -> Unit = {},
    onNavigateToSession: (sessionId: String) -> Unit = {},
    onOpenTasksBoard: () -> Unit = {},
    isDarkTheme: Boolean = true,
    onToggleTheme: () -> Unit = {},
    applySystemInsets: Boolean = true,
    isMultiPane: Boolean = false,
    onOpenNavRail: () -> Unit = {},
    showTodoSheet: Boolean = true,
    viewModel: ChatViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    val commands by viewModel.commands.collectAsStateWithLifecycle()
    val providers by viewModel.providers.collectAsStateWithLifecycle()
    val agents by viewModel.agents.collectAsStateWithLifecycle()
    val selectedModel by viewModel.selectedModel.collectAsStateWithLifecycle()
    val selectedAgent by viewModel.selectedAgent.collectAsStateWithLifecycle()
    val listState = rememberLazyListState()

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
    val clipboard = LocalClipboardManager.current

    // The strip shows the user's explicit pick if any, else the last-run state from the stream.
    val displayAgent = selectedAgent ?: uiState.agentMode
    val displayModelRef = selectedModel ?: run {
        val p = uiState.providerID
        val m = uiState.modelID
        if (p != null && m != null) ModelRef(providerID = p, modelID = m) else null
    }
    val displayModel = displayModelRef?.modelID
    val displayProvider = displayModelRef?.providerID

    Scaffold(
        containerColor = Surface,
        floatingActionButton = {
            if (!isMultiPane && sessionDirectory != null) {
                FloatingActionButton(
                    onClick = { onOpenTerminal(sessionDirectory) },
                    containerColor = MaterialTheme.colorScheme.secondaryContainer,
                    contentColor = MaterialTheme.colorScheme.onSecondaryContainer,
                ) {
                    Icon(Icons.Default.Terminal, contentDescription = "Open Terminal")
                }
            }
        },
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
                    if (isMultiPane) {
                        IconButton(onClick = onOpenNavRail, modifier = Modifier.size(42.dp)) {
                            Icon(
                                Icons.Default.Menu,
                                contentDescription = "Navigation menu",
                                tint = OnSurface,
                                modifier = Modifier.size(21.dp),
                            )
                        }
                    } else {
                        IconButton(onClick = onNavigateBack, modifier = Modifier.size(42.dp)) {
                            Icon(
                                Icons.AutoMirrored.Filled.ArrowBack,
                                contentDescription = "Back",
                                tint = OnSurface,
                                modifier = Modifier.size(21.dp),
                            )
                        }
                    }
                    Column(modifier = Modifier.weight(1f).padding(horizontal = 4.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(
                                text = uiState.session?.title ?: "Session",
                                fontSize = 15.sp,
                                fontWeight = FontWeight.Medium,
                                color = OnSurface,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.weight(1f, fill = false),
                            )
                            if (uiState.sessionStatus == "busy") {
                                Spacer(Modifier.width(8.dp))
                                CircularProgressIndicator(
                                    modifier = Modifier.size(12.dp),
                                    strokeWidth = 1.5.dp,
                                    color = Secondary,
                                )
                            }
                        }
                        uiState.session?.directory?.let { dir ->
                            Text(
                                text = dir,
                                fontFamily = ForgeMono,
                                fontSize = 11.5.sp,
                                color = OnSurfaceFaint,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                    }
                    if (isMultiPane) {
                        // Mode badge + model — right panel shows full session info so only compact version here.
                        Text(
                            text = (displayAgent ?: "build").replaceFirstChar { it.uppercase() },
                            fontFamily = ForgeMono,
                            fontSize = 11.sp,
                            fontWeight = FontWeight.Bold,
                            color = OnPrimary,
                            modifier = Modifier
                                .clip(ForgeShapes.xs)
                                .background(Primary)
                                .padding(horizontal = 6.dp, vertical = 2.dp),
                        )
                        if (displayModel != null) {
                            Spacer(Modifier.width(6.dp))
                            Text(
                                text = displayModel,
                                fontFamily = ForgeMono,
                                fontSize = 11.sp,
                                color = OnSurfaceVariant,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.widthIn(max = 100.dp),
                            )
                        }
                        Spacer(Modifier.width(2.dp))
                    } else {
                        IconButton(onClick = { showInfoSheet = true }, modifier = Modifier.size(42.dp)) {
                            Icon(Icons.Default.Info, contentDescription = "Session info", tint = OnSurfaceVariant, modifier = Modifier.size(20.dp))
                        }
                    }
                    Box {
                        IconButton(onClick = { showOverflow = true }, modifier = Modifier.size(42.dp)) {
                            Icon(Icons.Default.MoreVert, contentDescription = "More", tint = OnSurfaceVariant, modifier = Modifier.size(20.dp))
                        }
                        OverflowMenu(
                            expanded = showOverflow,
                            onDismiss = { showOverflow = false },
                            isDarkTheme = isDarkTheme,
                            isShared = uiState.session?.share != null,
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
                                viewModel.archiveSession { onNavigateBack() }
                            },
                            onDelete = {
                                showOverflow = false
                                viewModel.deleteSession { onNavigateBack() }
                            },
                            onToggleTheme = {
                                showOverflow = false
                                onToggleTheme()
                            },
                        )
                    }
                }
                HorizontalDivider(color = Hairline, thickness = 1.dp)
            }
        },
        bottomBar = {
            // Surface fills behind the system bars; content is inset above the
            // gesture-nav bar (and the IME when open) so it isn't clipped on
            // rounded / gesture-nav screens.
            Column(
                Modifier
                    .background(Surface)
                    .windowInsetsPadding(WindowInsets.ime.union(WindowInsets.navigationBars)),
            ) {
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
                HorizontalDivider(color = Hairline)
                PromptInput(
                    onSend = { text, attachments -> viewModel.sendPrompt(text, attachments) },
                    enabled = pendingPermission == null && pendingQuestion == null,
                    busy = uiState.sessionStatus == "busy",
                    onStop = { viewModel.abort() },
                    commands = commands,
                    onSearchFiles = { query -> viewModel.searchFiles(query) },
                    onRunCommand = { name, args -> viewModel.runCommand(name, args) },
                )
            }
        },
    ) { padding ->
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            LazyColumn(
                state = listState,
                reverseLayout = true, // newest at the bottom (index 0); see sticky-scroll note above
                contentPadding = PaddingValues(top = 6.dp, bottom = 64.dp), // 6+8 ≈ 14dp top gutter; clear the sheet peek
                modifier = Modifier
                    .fillMaxHeight()
                    .fillMaxWidth()
                    .widthIn(max = 720.dp) // tablet: cap + center the stream
                    .align(Alignment.TopCenter)
                    .nestedScroll(autoScrollConnection)
                    .imeNestedScroll(),
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

            // Todos dock — only in single/medium pane; moves to info panel in expanded.
            if (showTodoSheet) {
                TodoSheet(
                    todos = uiState.todos,
                    onOpenTasksBoard = onOpenTasksBoard,
                )
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
    isDarkTheme: Boolean,
    isShared: Boolean,
    onRename: () -> Unit,
    onFork: () -> Unit,
    onSummarize: () -> Unit,
    onShare: () -> Unit,
    onArchive: () -> Unit,
    onDelete: () -> Unit,
    onToggleTheme: () -> Unit,
) {
    DropdownMenu(
        expanded = expanded,
        onDismissRequest = onDismiss,
        containerColor = SurfaceContainerHigh,
    ) {
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
        DropdownMenuItem(
            text = { Text(if (isDarkTheme) "Light theme" else "Dark theme", color = OnSurface) },
            leadingIcon = {
                Icon(
                    if (isDarkTheme) Icons.Default.LightMode else Icons.Default.DarkMode,
                    contentDescription = null,
                    tint = OnSurfaceVariant,
                )
            },
            onClick = onToggleTheme,
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
    Column(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
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
            fontFamily = ForgeMono,
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
