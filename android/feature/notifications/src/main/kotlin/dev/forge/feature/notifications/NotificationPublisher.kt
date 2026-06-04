package dev.forge.feature.notifications

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.getSystemService

/**
 * Renders a daemon push into a system notification whose tap deep-links into the
 * relevant Chat session.
 *
 * The relay sends FCM `data` messages (not `notification` messages) so delivery
 * is handled by [ForgeMessagingService] in all app states — we build the visible
 * notification here, which also lets the tap carry our deep-link extras.
 */
class NotificationPublisher(private val context: Context) {

    /**
     * Posts a notification for the given push. [sessionId] (may be blank) drives
     * the deep-link; [eventType] is the daemon event that triggered it.
     * No-ops silently when the user has denied the notification permission.
     */
    fun show(title: String, body: String, sessionId: String, eventType: String?) {
        ensureChannel()
        val launch = launchIntent(sessionId, eventType)
        val pending = PendingIntent.getActivity(
            context,
            sessionId.hashCode(),
            launch,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val notification = NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_notify_chat)
            .setContentTitle(title.ifBlank { "Forge" })
            .setContentText(body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(body))
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setAutoCancel(true)
            .setContentIntent(pending)
            .build()

        val manager = NotificationManagerCompat.from(context)
        if (!manager.areNotificationsEnabled()) return
        val notifId = if (sessionId.isNotBlank()) sessionId.hashCode() else FALLBACK_ID
        try {
            manager.notify(notifId, notification)
        } catch (_: SecurityException) {
            // POST_NOTIFICATIONS not granted (API 33+); drop silently.
        }
    }

    private fun launchIntent(sessionId: String, eventType: String?): Intent {
        val intent = context.packageManager
            .getLaunchIntentForPackage(context.packageName)
            ?: Intent(Intent.ACTION_MAIN)
        intent.setPackage(context.packageName)
        intent.addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        if (sessionId.isNotBlank()) {
            PushDeepLink.applyTo(intent, sessionId, eventType)
        }
        return intent
    }

    private fun ensureChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val manager = context.getSystemService<NotificationManager>() ?: return
        if (manager.getNotificationChannel(CHANNEL_ID) != null) return
        val channel = NotificationChannel(
            CHANNEL_ID,
            "Agent activity",
            NotificationManager.IMPORTANCE_HIGH,
        ).apply {
            description = "Agent finished, permission and question prompts"
        }
        manager.createNotificationChannel(channel)
    }

    private companion object {
        const val CHANNEL_ID = "forge_agent_activity"
        const val FALLBACK_ID = 1
    }
}
