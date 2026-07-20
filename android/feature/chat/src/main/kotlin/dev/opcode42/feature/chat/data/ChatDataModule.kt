package dev.opcode42.feature.chat.data

import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class ChatDataModule {
    @Binds
    abstract fun bindStashStore(impl: DataStoreStashStore): StashStore
}
