package dev.opcode42.app

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.core.content.ContextCompat
import dagger.hilt.android.AndroidEntryPoint
import dev.opcode42.app.navigation.Opcode42NavGraph
import dev.opcode42.feature.chat.ui.Opcode42Theme
import dev.opcode42.feature.notifications.PushController
import dev.opcode42.feature.notifications.PushDeepLink
import dev.opcode42.feature.settings.AppPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    @Inject
    lateinit var prefs: AppPreferences

    @Inject
    lateinit var pushController: PushController

    // Latest push deep-link tap, updated on launch + onNewIntent. Wrapped in a
    // monotonic token so repeat taps for the SAME session still re-navigate (the
    // token changes even when the session id does not).
    private data class DeepLinkTap(val token: Long, val target: PushDeepLink.Target)
    private val deepLink = MutableStateFlow<DeepLinkTap?>(null)
    private var deepLinkSeq = 0L

    private fun emitDeepLink(intent: Intent?) {
        val target = PushDeepLink.fromIntent(intent) ?: return
        deepLink.value = DeepLinkTap(deepLinkSeq++, target)
    }

    private val requestNotificationPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { /* result ignored */ }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        emitDeepLink(intent)
        maybeRequestNotificationPermission()
        setContent {
            val darkTheme by prefs.darkTheme.collectAsState(initial = true)
            val scope = rememberCoroutineScope()
            val tap by deepLink.collectAsState()
            val consumedToken = remember { mutableStateOf(-1L) }
            // Surface the tap once (keyed by token, so a repeat push for the same
            // session re-navigates). Cleared by bumping consumedToken on consume.
            val pending = tap?.takeIf { it.token != consumedToken.value }
            Opcode42Theme(darkTheme = darkTheme) {
                Opcode42NavGraph(
                    isDarkTheme = darkTheme,
                    onToggleTheme = { scope.launch { prefs.setDarkTheme(!darkTheme) } },
                    deepLinkSessionId = pending?.target?.sessionId,
                    deepLinkToken = pending?.token ?: -1L,
                    onDeepLinkConsumed = { consumedToken.value = pending?.token ?: -1L },
                )
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        emitDeepLink(intent)
    }

    private fun maybeRequestNotificationPermission() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        if (!pushController.isPushAvailable) return
        val granted = ContextCompat.checkSelfPermission(
            this, Manifest.permission.POST_NOTIFICATIONS,
        ) == PackageManager.PERMISSION_GRANTED
        if (!granted) requestNotificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
    }
}
