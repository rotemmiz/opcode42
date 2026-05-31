package dev.forge.feature.chat.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.CallSplit
import androidx.compose.material.icons.filled.DarkMode
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.LightMode
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Terminal
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.forge.core.model.Message
import dev.forge.core.model.Part
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
    viewModel: ChatViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    val commands by viewModel.commands.collectAsStateWithLifecycle()
    val listState = rememberLazyListState()

    // Only auto-scroll if the user is already near the bottom
    val atBottom by remember {
        derivedStateOf {
            val info = listState.layoutInfo
            val last = info.visibleItemsInfo.lastOrNull() ?: return@derivedStateOf true
            last.index >= info.totalItemsCount - 2
        }
    }
    val totalItems = uiState.messages.size + uiState.optimisticMessages.size
    LaunchedEffect(totalItems) {
        if (totalItems > 0 && atBottom) {
            listState.animateScrollToItem(totalItems - 1)
        }
    }

    // Show permission sheet if any are pending
    val pendingPermission = uiState.pendingPermissions.firstOrNull()
    val pendingQuestion = uiState.pendingQuestions.firstOrNull()

    val sessionDirectory = uiState.session?.directory
    var showInfoSheet by remember { mutableStateOf(false) }
    var showOverflow by remember { mutableStateOf(false) }

    Scaffold(
        containerColor = Surface,
        floatingActionButton = {
            if (sessionDirectory != null) {
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
            Column {
                TopAppBar(
                    title = {
                        Column {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text(
                                    text = uiState.session?.title ?: "Session",
                                    style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.Medium),
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
                                    fontFamily = FontFamily.Monospace,
                                    fontSize = 11.5.sp,
                                    color = OnSurfaceFaint,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                    },
                    navigationIcon = {
                        IconButton(onClick = onNavigateBack) {
                            Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back", tint = OnSurface)
                        }
                    },
                    actions = {
                        IconButton(onClick = { showInfoSheet = true }) {
                            Icon(Icons.Default.Info, contentDescription = "Session info", tint = OnSurfaceVariant)
                        }
                        Box {
                            IconButton(onClick = { showOverflow = true }) {
                                Icon(Icons.Default.MoreVert, contentDescription = "More", tint = OnSurfaceVariant)
                            }
                            OverflowMenu(
                                expanded = showOverflow,
                                onDismiss = { showOverflow = false },
                                isDarkTheme = isDarkTheme,
                                onFork = {
                                    showOverflow = false
                                    viewModel.forkSession { newId -> onNavigateToSession(newId) }
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
                    },
                    colors = TopAppBarDefaults.topAppBarColors(
                        containerColor = Surface,
                        titleContentColor = OnSurface,
                    ),
                )
                HorizontalDivider(color = Hairline, thickness = 1.dp)
            }
        },
        bottomBar = {
            Column(Modifier.background(Surface)) {
                HorizontalDivider(color = Hairline)
                StatusStrip(
                    mode = uiState.agentMode,
                    model = uiState.modelID,
                    provider = uiState.providerID,
                    tokens = uiState.session?.tokens,
                )
                HorizontalDivider(color = Hairline)
                PromptInput(
                    onSend = { text, attachments -> viewModel.sendPrompt(text, attachments) },
                    enabled = pendingPermission == null && pendingQuestion == null,
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
                contentPadding = PaddingValues(bottom = 64.dp), // clear the todo-sheet peek
                modifier = Modifier
                    .fillMaxHeight()
                    .fillMaxWidth()
                    .widthIn(max = 720.dp) // tablet: cap + center the stream
                    .align(Alignment.TopCenter)
                    .imeNestedScroll(),
            ) {
                items(uiState.messages, key = { it.id }) { message ->
                    // SSE live parts supersede REST-loaded parts when present
                    val liveParts = uiState.parts[message.id]
                    MessageBlock(
                        message = message,
                        parts = if (liveParts != null) liveParts else message.parts,
                        diffs = uiState.diffs,
                    )
                }
                items(uiState.optimisticMessages, key = { "opt:${it.id}" }) { opt ->
                    OptimisticMessageBlock(opt)
                }
            }

            // Todos dock — anchored above the status strip / composer
            TodoSheet(
                todos = uiState.todos,
                onOpenTasksBoard = onOpenTasksBoard,
            )
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
    }
}

@Composable
private fun OverflowMenu(
    expanded: Boolean,
    onDismiss: () -> Unit,
    isDarkTheme: Boolean,
    onFork: () -> Unit,
    onDelete: () -> Unit,
    onToggleTheme: () -> Unit,
) {
    DropdownMenu(
        expanded = expanded,
        onDismissRequest = onDismiss,
        containerColor = SurfaceContainerHigh,
    ) {
        DropdownMenuItem(
            text = { Text("Fork session", color = OnSurface) },
            leadingIcon = { Icon(Icons.Default.CallSplit, contentDescription = null, tint = OnSurfaceVariant) },
            onClick = onFork,
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
            fontFamily = FontFamily.Monospace,
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
        }
    }
}

@Composable
private fun UserMessageBlock(
    parts: List<Part>,
    diffs: Map<String, List<SnapshotFileDiff>> = emptyMap(),
) {
    Row(modifier = Modifier.fillMaxWidth()) {
        // 2dp primary blue left accent bar
        Box(modifier = Modifier.width(2.dp).fillMaxHeight().background(Primary))
        Column(modifier = Modifier.padding(start = 13.dp, end = 14.dp)) {
            StreamParts(parts, diffs)
        }
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
    Row(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        Box(modifier = Modifier.width(2.dp).fillMaxHeight().background(Primary))
        Column(modifier = Modifier.padding(start = 13.dp, end = 14.dp)) {
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
}
