package dev.forge.feature.notifications

import android.content.ComponentName
import android.content.Context
import android.content.pm.PackageManager
import android.util.Log
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import javax.inject.Inject
import javax.inject.Singleton

/**
 * App-facing entry point for push. The app calls [start] once on launch and
 * [logout] on sign-out/teardown. Everything is gated on Firebase being configured
 * for this build ([PushConfig]) so the no-google-services build is a clean no-op.
 */
@Singleton
class PushController @Inject constructor(
    @ApplicationContext private val context: Context,
    private val registrar: PushRegistrar,
) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    /** True when this build can do live FCM (a Firebase config is present). */
    val isPushAvailable: Boolean
        get() = PushConfig.isConfigured(context)

    /**
     * Initializes Firebase (if configured), enables the FCM receiver component,
     * and registers the current token with the daemon. No-ops without config.
     * Safe to call on every app start / foreground.
     */
    fun start() {
        if (!PushConfig.ensureInitialized(context)) {
            Log.d(TAG, "Push not configured; skipping FCM init")
            return
        }
        setMessagingServiceEnabled(true)
        scope.launch { registrar.sync() }
    }

    /** Unregisters from the daemon (fire-and-forget). Call on logout / server removal. */
    fun logout() {
        scope.launch { registrar.unregister() }
    }

    /**
     * Suspending unregister: DELETEs this device's registration and clears the
     * local token marker before returning. Used when the caller must sequence the
     * DELETE before switching the active server (so it targets the daemon being
     * left). No-ops when push is unavailable.
     */
    suspend fun logoutAndAwait() {
        registrar.unregister()
    }

    private fun setMessagingServiceEnabled(enabled: Boolean) {
        val component = ComponentName(context, ForgeMessagingService::class.java)
        val state = if (enabled) {
            PackageManager.COMPONENT_ENABLED_STATE_ENABLED
        } else {
            PackageManager.COMPONENT_ENABLED_STATE_DISABLED
        }
        runCatching {
            context.packageManager.setComponentEnabledSetting(
                component, state, PackageManager.DONT_KILL_APP,
            )
        }.onFailure { Log.w(TAG, "Could not toggle FCM service: ${it.message}") }
    }

    private companion object {
        const val TAG = "PushController"
    }
}
