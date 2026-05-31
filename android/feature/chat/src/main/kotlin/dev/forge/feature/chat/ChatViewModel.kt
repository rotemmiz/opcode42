package dev.forge.feature.chat

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.forge.core.model.*
import dev.forge.core.network.SseManager
import dev.forge.core.sdk.ForgeClient
import dev.forge.core.store.AppStore
import dev.forge.core.store.OptimisticMessage
import dev.forge.core.store.ConnectionState
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
import javax.inject.Inject

data class ChatUiState(
    val session: Session? = null,
    val messages: List<Message> = emptyList(),
    val parts: Map<String, List<Part>> = emptyMap(),
    val diffs: Map<String, List<SnapshotFileDiff>> = emptyMap(),
    val optimisticMessages: List<OptimisticMessage> = emptyList(),
    val pendingPermissions: List<PermissionRequest> = emptyList(),
    val pendingQuestions: List<QuestionRequest> = emptyList(),
    val sessionStatus: String = "idle",
    val isLoading: Boolean = false,
    val isSending: Boolean = false,
)

@HiltViewModel
class ChatViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val client: ForgeClient,
    private val store: AppStore,
    private val sseManager: SseManager,
) : ViewModel() {

    private val sessionId: String = checkNotNull(savedStateHandle["sessionId"])

    // C4 — tracks in-flight diff fetches to prevent duplicate requests and infinite retry
    private val _diffInFlight = mutableSetOf<String>()

    val uiState: StateFlow<ChatUiState> = store.state
        .map { appState ->
            val session = appState.sessions.firstOrNull { it.id == sessionId }
            ChatUiState(
                session = session,
                messages = appState.messages[sessionId] ?: emptyList(),
                parts = appState.parts,
                diffs = appState.diffs,
                optimisticMessages = appState.optimisticMessages[sessionId] ?: emptyList(),
                pendingPermissions = appState.permissions[sessionId] ?: emptyList(),
                pendingQuestions = appState.questions[sessionId] ?: emptyList(),
                sessionStatus = appState.sessionStatus[sessionId] ?: "idle",
            )
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ChatUiState())

    private val directory: String? get() = uiState.value.session?.directory

    init {
        loadMessages()
        // Subscribe to per-directory SSE exactly once, when the session's directory is known
        viewModelScope.launch {
            val dir = uiState.first { it.session?.directory != null }.session?.directory
            if (dir != null) sseManager.subscribeDirectory(dir)
        }
        // Reload messages after a reconnection (GlobalDisposed wipes state)
        viewModelScope.launch {
            store.state
                .map { it.connectionState }
                .distinctUntilChanged()
                .drop(1) // skip initial state; init already called loadMessages()
                .filter { it is ConnectionState.Connected }
                .collect { loadMessages() }
        }
        // C4 — Watch for new PatchParts and load diff content for each one not yet fetched
        viewModelScope.launch {
            store.state.collect { appState ->
                val dir = appState.sessions.firstOrNull { it.id == sessionId }?.directory ?: return@collect
                appState.parts[sessionId]
                    ?.filterIsInstance<PatchPart>()
                    ?.filter { it.messageID !in appState.diffs && it.messageID !in _diffInFlight }
                    ?.forEach { patch ->
                        _diffInFlight.add(patch.messageID)
                        loadDiff(patch.messageID, dir)
                    }
            }
        }
    }

    private fun loadMessages() {
        viewModelScope.launch {
            try {
                val messages = client.getMessages(sessionId, directory)
                android.util.Log.d("ChatVM", "loadMessages: got ${messages.size} messages for $sessionId")
                messages.forEach { msg ->
                    android.util.Log.d("ChatVM", "  dispatch msg ${msg.id} role=${msg.role} parts=${msg.parts.size}")
                    store.dispatch(AppEvent.MessageUpdated(msg))
                }
            } catch (e: Exception) {
                android.util.Log.e("ChatVM", "loadMessages failed", e)
            }
        }
    }

    /** C4 — Fetch unified diff for a message and store in AppState */
    private fun loadDiff(messageId: String, directory: String) {
        viewModelScope.launch {
            try {
                val diffs = client.getSessionDiff(sessionId, messageId, directory)
                store.dispatch(AppEvent.SessionDiffLoaded(messageId, diffs))
            } catch (e: Exception) {
                android.util.Log.w("ChatVM", "loadDiff failed for $messageId", e)
                // Store empty list to prevent infinite retry on persistent failures
                store.dispatch(AppEvent.SessionDiffLoaded(messageId, emptyList()))
            } finally {
                _diffInFlight.remove(messageId)
            }
        }
    }

    /** A7 — Optimistic prompt submit */
    fun sendPrompt(text: String) {
        viewModelScope.launch {
            val optimisticId = store.addOptimistic(sessionId, text)
            try {
                client.sendPrompt(sessionId, text, directory)
            } catch (e: Exception) {
                store.removeOptimistic(sessionId, optimisticId)
            }
        }
    }

    /** A8 — Permission reply */
    fun replyPermission(requestId: String, allow: Boolean) {
        viewModelScope.launch {
            try {
                client.replyPermission(requestId, allow)
                store.dispatch(AppEvent.PermissionReplied(requestId))
            } catch (_: Exception) { }
        }
    }

    /** A8 — Question reply */
    fun replyQuestion(requestId: String, answer: String) {
        viewModelScope.launch {
            try {
                client.replyQuestion(requestId, answer)
                store.dispatch(AppEvent.QuestionReplied(requestId))
            } catch (_: Exception) { }
        }
    }

    fun rejectQuestion(requestId: String) {
        viewModelScope.launch {
            try {
                client.rejectQuestion(requestId)
                store.dispatch(AppEvent.QuestionRejected(requestId))
            } catch (_: Exception) { }
        }
    }
}
