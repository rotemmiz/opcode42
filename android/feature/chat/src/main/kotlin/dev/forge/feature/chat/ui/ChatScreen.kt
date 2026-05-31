package dev.forge.feature.chat.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Info
import androidx.compose.material3.*
import androidx.compose.runtime.*
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
import dev.forge.core.store.OptimisticMessage
import dev.forge.feature.chat.ChatViewModel
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatScreen(
    sessionId: String,
    onNavigateBack: () -> Unit,
    viewModel: ChatViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()

    // Auto-scroll to bottom when new messages arrive
    val totalItems = uiState.messages.size + uiState.optimisticMessages.size
    LaunchedEffect(totalItems) {
        if (totalItems > 0) {
            scope.launch { listState.animateScrollToItem(totalItems - 1) }
        }
    }

    // Show permission sheet if any are pending
    val pendingPermission = uiState.pendingPermissions.firstOrNull()
    val pendingQuestion = uiState.pendingQuestions.firstOrNull()

    Scaffold(
        containerColor = Surface,
        topBar = {
            Column {
                TopAppBar(
                    title = {
                        Column {
                            Text(
                                text = uiState.session?.title ?: "Session",
                                style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.Medium),
                                color = OnSurface,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
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
                        IconButton(onClick = { /* session info sheet — Phase C */ }) {
                            Icon(Icons.Default.Info, contentDescription = "Info", tint = OnSurfaceVariant)
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
                PromptInput(
                    onSend = viewModel::sendPrompt,
                    enabled = pendingPermission == null && pendingQuestion == null,
                )
            }
        },
    ) { padding ->
        LazyColumn(
            state = listState,
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(bottom = 8.dp),
        ) {
            items(uiState.messages, key = { it.id }) { message ->
                MessageBlock(
                    message = message,
                    parts = uiState.parts[message.id] ?: emptyList(),
                )
            }
            items(uiState.optimisticMessages, key = { "opt:${it.id}" }) { opt ->
                OptimisticMessageBlock(opt)
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
    }
}

@Composable
private fun MessageBlock(message: Message, parts: List<Part>) {
    Column(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        if (message.role == "user") {
            UserMessageBlock(message, parts)
        } else {
            AssistantMessageBlock(message, parts)
        }
    }
}

@Composable
private fun UserMessageBlock(message: Message, parts: List<Part>) {
    Row(modifier = Modifier.fillMaxWidth()) {
        // 2dp primary blue left accent bar
        Box(modifier = Modifier.width(2.dp).fillMaxHeight().background(Primary))
        Column(modifier = Modifier.padding(start = 13.dp, end = 14.dp)) {
            parts.forEach { part -> PartRenderer(part) }
        }
    }
}

@Composable
private fun AssistantMessageBlock(message: Message, parts: List<Part>) {
    Column(modifier = Modifier.padding(horizontal = 0.dp)) {
        parts.forEach { part -> PartRenderer(part) }
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
