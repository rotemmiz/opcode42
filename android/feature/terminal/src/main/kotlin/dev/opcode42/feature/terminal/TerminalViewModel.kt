package dev.opcode42.feature.terminal

import android.util.Base64
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.opcode42.core.sdk.Opcode42Client
import dev.opcode42.core.sdk.PtyClient
import dev.opcode42.core.sdk.TerminalEmulator
import dev.opcode42.feature.connections.ServerConnectionManager
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

@HiltViewModel
class TerminalViewModel @Inject constructor(
    private val client: Opcode42Client,
    private val connectionManager: ServerConnectionManager,
) : ViewModel() {

    private var ptyClient: PtyClient? = null
    private var connectJob: Job? = null
    private var cursorJob: Job? = null

    /** PTY session id, kept so we can resize and reconnect to the same shell. */
    private var ptyId: String? = null
    /** Last server cursor seen (UTF-16 code units) — used to resume on reconnect. */
    @Volatile private var lastCursor: Long? = null

    private val emulator = TerminalEmulator()

    /**
     * Monotonic counter bumped on every emulator mutation so Compose recomposes.
     * [lines] re-reads the emulator snapshot whenever this changes; this avoids
     * holding a second copy of the screen buffer in a SnapshotStateList.
     */
    var revision by mutableStateOf(0L)
        private set

    /** Current rendered terminal lines (snapshot of the emulator). */
    val lines: List<String>
        get() {
            @Suppress("UNUSED_EXPRESSION") revision // read to subscribe to changes
            return emulator.render()
        }

    private val _connected = MutableStateFlow(false)
    val connected: StateFlow<Boolean> = _connected

    fun connect(directory: String) {
        if (connectJob?.isActive == true) return // already connected/connecting
        connectJob = viewModelScope.launch {
            try {
                // 1. Create the PTY session once; reuse its id across reconnects.
                val id = ptyId ?: client.createPty(directory).id.also { ptyId = it }

                // 2. Build the auth token: base64(user:pass) or base64(:token).
                val conn = connectionManager.active
                    ?: run { Log.e("TerminalVM", "No active connection"); return@launch }
                val authToken = buildAuthToken(conn.http.username, conn.http.password)

                // 3. Connect the WebSocket, resuming from the last cursor if any.
                val pc = client.connectPty(id, authToken, lastCursor)
                ptyClient = pc
                _connected.value = true

                // 4. Track the server cursor for reconnect resume.
                cursorJob = launch {
                    pc.cursor.collect { lastCursor = it }
                }

                // 5. Feed output through the emulator; channel close (EOF) ends collect.
                pc.output.collect { chunk ->
                    emulator.feed(chunk)
                    bumpRevision()
                }
                _connected.value = false
            } catch (e: Exception) {
                Log.e("TerminalVM", "connect failed", e)
                _connected.value = false
            } finally {
                cursorJob?.cancel()
                cursorJob = null
            }
        }
    }

    private fun buildAuthToken(username: String?, password: String?): String {
        val raw = when {
            username != null && password != null -> "$username:$password"
            password != null -> ":$password"
            else -> return ""
        }
        return Base64.encodeToString(raw.toByteArray(Charsets.UTF_8), Base64.NO_WRAP)
    }

    private fun bumpRevision() {
        revision += 1
    }

    /** Sends user keystrokes to the shell (UTF-8 bytes). */
    fun sendInput(text: String) {
        ptyClient?.send(text.toByteArray(Charsets.UTF_8))
    }

    /** Last size reported to the daemon — guards against redundant PUT /pty calls. */
    private var lastSize: Pair<Int, Int>? = null

    /** Reports the visible terminal size to the daemon so wrapping matches the view. */
    fun resize(rows: Int, cols: Int) {
        val id = ptyId ?: return
        if (rows <= 0 || cols <= 0) return
        val size = rows to cols
        if (size == lastSize) return // IME/scroll re-layouts fire identical sizes; skip
        lastSize = size
        viewModelScope.launch {
            runCatching { client.resizePty(id, rows, cols) }
                .onFailure { Log.w("TerminalVM", "resize failed", it) }
        }
    }

    override fun onCleared() {
        connectJob?.cancel()
        cursorJob?.cancel()
        ptyClient?.close()
        super.onCleared()
    }
}
