package dev.forge.feature.notifications

import android.content.Context
import dagger.Binds
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object NotificationsModule {
    @Provides
    @Singleton
    fun providePushTokenProvider(@ApplicationContext context: Context): PushTokenProvider =
        FirebaseTokenProvider(context)
}

@Module
@InstallIn(SingletonComponent::class)
abstract class NotificationsBindings {
    @Binds
    abstract fun bindPushIdentityStore(impl: PushPreferences): PushIdentityStore
}
