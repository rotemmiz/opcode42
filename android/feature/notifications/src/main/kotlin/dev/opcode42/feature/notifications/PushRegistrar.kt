package dev.opcode42.feature.notifications

import android.util.Log
import dev.opcode42.core.sdk.BaseUrlProvider
import dev.opcode42.core.sdk.Opcode42Client
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Owns the device's push-registration lifecycle against the daemon relay
 * (plan 13 §13.8 / `POST|DELETE /push/register`).
 *
 * Responsibilities:
 *  - [sync]: obtain the current FCM token and register it with the daemon iff it
 *    changed since the last successful registration (cheap to call on app start
 *    and on foreground).
 *  - [onTokenRefreshed]: FCM rotated the token → re-register the new one.
 *  - [unregister]: logout/teardown → DELETE the registration so the daemon stops
 *    pushing to a token we no longer own.
 *
 * All paths are no-ops when push is not configured (no FCM token) or when there
 * is no active server to register against — the app must still run on the
 * no-google-services build.
 */
@Singleton
class PushRegistrar @Inject constructor(
    private val client: Opcode42Client,
    private val prefs: PushIdentityStore,
    private val tokenProvider: PushTokenProvider,
    private val baseUrlProvider: BaseUrlProvider,
) {
    private val hasActiveServer: Boolean
        get() = baseUrlProvider.baseUrl != null

    /**
     * Registers the current FCM token with the daemon if it differs from the
     * last registered token. Returns true when a (re)registration was performed.
     */
    suspend fun sync(): Boolean {
        if (!hasActiveServer) return false
        val token = try {
            tokenProvider.currentToken()
        } catch (t: Throwable) {
            Log.w(TAG, "FCM token fetch failed: ${t.message}")
            null
        } ?: return false

        if (token == prefs.registeredToken()) return false
        return register(token)
    }

    /** Re-registers after FCM reports a rotated token. */
    suspend fun onTokenRefreshed(token: String) {
        if (!hasActiveServer) {
            // No server yet — drop the stale mapping so the next sync re-registers.
            prefs.setRegisteredToken(null)
            return
        }
        register(token)
    }

    private suspend fun register(token: String): Boolean {
        return try {
            client.registerPush(deviceId = prefs.deviceId(), fcmToken = token)
            prefs.setRegisteredToken(token)
            Log.d(TAG, "Registered push device with daemon")
            true
        } catch (t: Throwable) {
            Log.w(TAG, "Push register failed: ${t.message}")
            false
        }
    }

    /**
     * Unregisters this device from the daemon (logout/teardown). Clears the local
     * registered-token marker regardless of network outcome so a later sync
     * re-registers cleanly.
     */
    suspend fun unregister() {
        val deviceId = prefs.deviceId()
        if (hasActiveServer) {
            try {
                client.unregisterPush(deviceId)
            } catch (t: Throwable) {
                Log.w(TAG, "Push unregister failed: ${t.message}")
            }
        }
        prefs.setRegisteredToken(null)
    }

    private companion object {
        const val TAG = "PushRegistrar"
    }
}
