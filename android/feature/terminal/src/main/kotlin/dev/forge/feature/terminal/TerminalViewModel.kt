package dev.forge.feature.terminal

import android.util.Base64
import android.util.Log
import androidx.compose.runtime.mutableStateListOf
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dev.forge.core.sdk.PtyClient
import dev.forge.core.sdk.ForgeClient
import dev.forge.feature.connections.ServerConnectionManager
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

@HiltViewModel
class TerminalViewModel @Inject constructor(
    private val client: ForgeClient,
    private val connectionManager: ServerConnectionManager,
) : ViewModel() {

    private var ptyClient: PtyClient? = null
    private var connectJob: Job? = null

    /** Rendered terminal lines — observed directly by the UI via Compose snapshot state. */
    val lines = mutableStateListOf<String>()

    private val _connected = MutableStateFlow(false)
    val connected: StateFlow<Boolean> = _connected

    fun connect(directory: String) {
        if (connectJob?.isActive == true) return  // already connected
        connectJob = viewModelScope.launch {
            try {
                // 1. Create PTY session via POST /pty
                val pty = client.createPty(directory)

                // 2. Build auth token: base64(user:pass) or base64(:token)
                val conn = connectionManager.active
                    ?: run { Log.e("TerminalVM", "No active connection"); return@launch }
                val http = conn.http
                val authToken = buildAuthToken(http.username, http.password)

                // 3. Connect to the PTY WebSocket — ForgeClient returns a ready PtyClient
                val pc = client.connectPty(pty.id, authToken)
                ptyClient = pc
                _connected.value = true

                // 4. Collect output and render into lines.
                // Channel close (EOF) causes collect to return naturally.
                pc.output.collect { bytes ->
                    appendText(String(bytes, Charsets.UTF_8))
                }
                // Flow completed = connection ended (channel closed by server or failure)
                _connected.value = false
            } catch (e: Exception) {
                Log.e("TerminalVM", "connect failed", e)
                _connected.value = false
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

    private fun appendText(text: String) {
        val segments = text.split("\n")
        segments.forEachIndexed { i, seg ->
            val clean = seg.trimEnd('\r')
            if (i == 0 && lines.isNotEmpty()) {
                lines[lines.lastIndex] = lines.last() + clean
            } else {
                lines.add(clean)
            }
        }
    }

    fun sendInput(text: String) {
        ptyClient?.send(text.toByteArray(Charsets.UTF_8))
    }

    override fun onCleared() {
        connectJob?.cancel()
        ptyClient?.close()
        super.onCleared()
    }
}
