package dev.opcode42.feature.connections

import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import dev.opcode42.core.network.ActiveConnectionProvider
import dev.opcode42.core.sdk.BaseUrlProvider
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class ConnectionsModule {
    @Binds
    @Singleton
    abstract fun bindActiveConnectionProvider(impl: ServerConnectionManager): ActiveConnectionProvider

    @Binds
    @Singleton
    abstract fun bindBaseUrlProvider(impl: ServerConnectionManager): BaseUrlProvider
}
