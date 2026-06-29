package dev.opcode42.feature.notifications

import android.content.Context
import com.google.firebase.messaging.FirebaseMessaging
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/**
 * Abstraction over "fetch the current FCM registration token". Pulled behind an
 * interface so [PushRegistrar] is unit-testable without a live Firebase project
 * (tests supply a fake; production uses [FirebaseTokenProvider]).
 */
interface PushTokenProvider {
    /** Current FCM token, or null when push is not configured / unavailable. */
    suspend fun currentToken(): String?
}

/**
 * Production [PushTokenProvider] backed by FirebaseMessaging. Returns null (rather
 * than throwing) when Firebase is not configured for this build, so the caller's
 * gating stays simple.
 */
class FirebaseTokenProvider(private val context: Context) : PushTokenProvider {
    override suspend fun currentToken(): String? {
        if (!PushConfig.ensureInitialized(context)) return null
        return suspendCancellableCoroutine { cont ->
            FirebaseMessaging.getInstance().token
                .addOnSuccessListener { token -> cont.resume(token) }
                .addOnFailureListener { e -> cont.resumeWithException(e) }
        }
    }
}
