package dev.forge.feature.notifications

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.first
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

private val Context.pushDataStore: DataStore<Preferences> by preferencesDataStore(name = "forge_push")

/**
 * The push-identity seam consumed by [PushRegistrar]: a stable per-install
 * device id and the last FCM token we successfully registered. Pulled behind an
 * interface so the registrar is unit-testable with an in-memory fake (no
 * DataStore / Android framework).
 */
interface PushIdentityStore {
    /** Stable per-install device id, generated and persisted on first access. */
    suspend fun deviceId(): String

    /** The last FCM token we registered with the daemon, or null if never. */
    suspend fun registeredToken(): String?

    suspend fun setRegisteredToken(token: String?)
}

/**
 * Persists this install's stable push identity: a generated [deviceId] (the key
 * the daemon upserts registrations by) and the last FCM token we successfully
 * registered, so token-refresh re-registers only when the token actually changed.
 */
@Singleton
class PushPreferences @Inject constructor(
    @ApplicationContext private val context: Context,
) : PushIdentityStore {
    private val deviceIdKey = stringPreferencesKey("device_id")
    private val registeredTokenKey = stringPreferencesKey("registered_token")

    /** Stable per-install device id, generated and persisted on first access. */
    override suspend fun deviceId(): String {
        val prefs = context.pushDataStore.data.first()
        prefs[deviceIdKey]?.let { return it }
        val generated = UUID.randomUUID().toString()
        context.pushDataStore.edit { it[deviceIdKey] = generated }
        return generated
    }

    /** The last FCM token we registered with the daemon, or null if never. */
    override suspend fun registeredToken(): String? =
        context.pushDataStore.data.first()[registeredTokenKey]

    override suspend fun setRegisteredToken(token: String?) {
        context.pushDataStore.edit { prefs ->
            if (token == null) prefs.remove(registeredTokenKey) else prefs[registeredTokenKey] = token
        }
    }
}
