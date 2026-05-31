package dev.forge.app

import android.app.Application
import dagger.hilt.android.HiltAndroidApp
import dev.forge.core.network.SseManager
import javax.inject.Inject

@HiltAndroidApp
class ForgeApp : Application() {
    @Inject
    lateinit var sseManager: SseManager

    override fun onCreate() {
        super.onCreate()
        sseManager.registerLifecycleObserver()
    }
}
