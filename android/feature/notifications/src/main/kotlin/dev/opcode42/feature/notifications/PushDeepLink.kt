package dev.opcode42.feature.notifications

import android.content.Intent

/**
 * Deep-link contract between a push notification and the app's launcher activity.
 *
 * The daemon relay sends an FCM `data` payload of `{event_type, session_id}`
 * (`internal/push/notification.go` Notification.data()). When the user taps the
 * notification we relaunch the app with these values as Intent extras; the
 * activity reads them via [fromIntent] and navigates to the relevant Chat screen.
 */
object PushDeepLink {
    const val EXTRA_SESSION_ID = "dev.opcode42.push.SESSION_ID"
    const val EXTRA_EVENT_TYPE = "dev.opcode42.push.EVENT_TYPE"

    /** FCM data keys as emitted by the daemon relay (internal/push/notification.go). */
    const val DATA_SESSION_ID = "session_id"
    const val DATA_EVENT_TYPE = "event_type"

    /**
     * A deep-link target parsed from a notification tap: the session to open and
     * the event that triggered the push (so the UI can, e.g., surface the pending
     * permission/question sheet).
     */
    data class Target(val sessionId: String, val eventType: String?)

    /** Stamps the deep-link extras onto [intent]. */
    fun applyTo(intent: Intent, sessionId: String, eventType: String?) {
        intent.putExtra(EXTRA_SESSION_ID, sessionId)
        if (eventType != null) intent.putExtra(EXTRA_EVENT_TYPE, eventType)
    }

    /** Extracts a [Target] from a launch intent, or null when no session is present. */
    fun fromIntent(intent: Intent?): Target? {
        val sessionId = intent?.getStringExtra(EXTRA_SESSION_ID) ?: return null
        if (sessionId.isBlank()) return null
        return Target(sessionId, intent.getStringExtra(EXTRA_EVENT_TYPE))
    }
}
