package dev.forge.feature.connections.discovery

import android.content.Context
import android.net.nsd.NsdManager
import android.net.wifi.WifiManager
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object DiscoveryModule {
    @Provides
    @Singleton
    fun provideNsdPlatform(@ApplicationContext context: Context): NsdPlatform {
        val nsd = context.getSystemService(Context.NSD_SERVICE) as NsdManager
        // WifiManager must be obtained from the application context to avoid leaking an Activity.
        val wifi = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
        return AndroidNsdPlatform(nsd, wifi)
    }
}
