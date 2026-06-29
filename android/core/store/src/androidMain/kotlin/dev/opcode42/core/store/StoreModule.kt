package dev.opcode42.core.store

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

/**
 * Provides the multiplatform [AppStore] as a Hilt `@Singleton` on Android.
 *
 * [AppStore] itself lives in commonMain and carries no DI annotations so it can
 * be shared with a future iOS target; this module is the Android-only wiring.
 */
@Module
@InstallIn(SingletonComponent::class)
object StoreModule {
    @Provides
    @Singleton
    fun provideAppStore(): AppStore = AppStore()
}
