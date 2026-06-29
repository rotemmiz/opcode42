package dev.opcode42.feature.notifications

import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import dagger.hilt.android.AndroidEntryPoint
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * Receives FCM messages from the daemon relay and surfaces them as notifications,
 * and re-registers when FCM rotates the device token.
 *
 * The relay sends `data`-only messages (`internal/push/notification.go`), so this
 * service is invoked for delivery in foreground, background, and (best-effort)
 * killed states, letting us attach the deep-link extras ourselves.
 */
@AndroidEntryPoint
class Opcode42MessagingService : FirebaseMessagingService() {

    @Inject
    lateinit var registrar: PushRegistrar

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    override fun onMessageReceived(message: RemoteMessage) {
        val data = message.data
        val sessionId = data[PushDeepLink.DATA_SESSION_ID].orEmpty()
        val eventType = data[PushDeepLink.DATA_EVENT_TYPE]

        // Prefer the FCM notification block (title/body) when present; the relay
        // currently sends data-only, so fall back to a sensible title per event.
        val title = message.notification?.title ?: titleFor(eventType)
        val body = message.notification?.body
            ?: data["body"]
            ?: defaultBodyFor(eventType)

        NotificationPublisher(applicationContext).show(title, body, sessionId, eventType)
    }

    override fun onNewToken(token: String) {
        scope.launch { registrar.onTokenRefreshed(token) }
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun titleFor(eventType: String?): String = when (eventType) {
        "permission.asked" -> "Permission needed"
        "question.asked" -> "Agent has a question"
        "session.idle" -> "Agent finished"
        else -> "Opcode42"
    }

    private fun defaultBodyFor(eventType: String?): String = when (eventType) {
        "permission.asked" -> "The agent needs your approval to continue."
        "question.asked" -> "The agent has a question for you."
        "session.idle" -> "Your agent finished its task."
        else -> "You have a new update."
    }
}
