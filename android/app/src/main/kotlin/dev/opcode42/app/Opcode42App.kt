package dev.opcode42.app

import android.app.Application
import dagger.hilt.android.HiltAndroidApp
import dev.opcode42.core.network.SseManager
import dev.opcode42.feature.notifications.PushController
import javax.inject.Inject

@HiltAndroidApp
class Opcode42App : Application() {
    @Inject
    lateinit var sseManager: SseManager

    @Inject
    lateinit var pushController: PushController

    override fun onCreate() {
        super.onCreate()
        sseManager.registerLifecycleObserver()
        // Register this device's FCM token with the daemon relay (plan 13 §13.8).
        // No-op on the no-google-services build (push not configured).
        pushController.start()
    }
}
