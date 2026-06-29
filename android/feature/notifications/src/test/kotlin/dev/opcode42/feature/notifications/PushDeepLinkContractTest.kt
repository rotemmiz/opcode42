package dev.opcode42.feature.notifications

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Pins the FCM `data` keys the client reads to the keys the daemon relay emits in
 * `internal/push/notification.go` Notification.data(): `{event_type, session_id}`.
 * A drift here silently breaks deep-linking, so it is locked by a test.
 */
class PushDeepLinkContractTest {

    @Test
    fun dataKeysMatchDaemonRelay() {
        assertEquals("session_id", PushDeepLink.DATA_SESSION_ID)
        assertEquals("event_type", PushDeepLink.DATA_EVENT_TYPE)
    }
}
