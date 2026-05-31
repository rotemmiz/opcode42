package dev.forge.feature.connections

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import dagger.hilt.android.qualifiers.ApplicationContext
import dev.forge.core.network.ActiveConnectionProvider
import dev.forge.core.network.HttpConfig
import dev.forge.core.network.ServerConnectionConfig
import dev.forge.core.sdk.BaseUrlProvider
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import javax.inject.Inject
import javax.inject.Singleton

private const val PREFS_FILE = "forge_server_connections"
private const val KEY_CONNECTIONS = "connections"
private const val KEY_ACTIVE = "active_key"

@Singleton
class ServerConnectionManager @Inject constructor(
    @ApplicationContext private val context: Context,
) : ActiveConnectionProvider, BaseUrlProvider {

    private val prefs: SharedPreferences by lazy {
        val masterKey = MasterKey.Builder(context)
            .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
            .build()
        EncryptedSharedPreferences.create(
            context,
            PREFS_FILE,
            masterKey,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
        )
    }

    private val _connections = MutableStateFlow<List<ServerConnection>>(emptyList())
    val connections: StateFlow<List<ServerConnection>> = _connections.asStateFlow()

    private val _active = MutableStateFlow<ServerConnection?>(null)
    val activeFlow: StateFlow<ServerConnection?> = _active.asStateFlow()

    init {
        load()
    }

    override val active: ServerConnectionConfig?
        get() = _active.value?.let { conn ->
            ServerConnectionConfig(url = conn.http.url, http = HttpConfig(
                url = conn.http.url,
                username = conn.http.username,
                password = conn.http.password,
            ))
        }

    override val baseUrl: String?
        get() = _active.value?.http?.url

    fun add(rawUrl: String, username: String? = null, password: String? = null, displayName: String? = null) {
        val normalized = normalizeServerUrl(rawUrl)
        val conn = ServerConnection.Http(
            http = HttpConfig(normalized, username, password),
            displayName = displayName,
        )
        val current = _connections.value
        if (current.any { it.key() == conn.key() }) return  // deduplicate
        _connections.value = current + conn
        if (_active.value == null) _active.value = conn
        persist()
    }

    fun remove(key: String) {
        _connections.value = _connections.value.filter { it.key() != key }
        if (_active.value?.key() == key) {
            _active.value = _connections.value.firstOrNull()
        }
        persist()
    }

    fun setActive(key: String) {
        _active.value = _connections.value.firstOrNull { it.key() == key }
        prefs.edit().putString(KEY_ACTIVE, key).apply()
    }

    // ─── Persistence ──────────────────────────────────────────────────────────

    private fun persist() {
        val list = _connections.value.filterIsInstance<ServerConnection.Http>().map { it.toPersisted() }
        prefs.edit()
            .putString(KEY_CONNECTIONS, Json.encodeToString(list))
            .putString(KEY_ACTIVE, _active.value?.key())
            .apply()
    }

    private fun load() {
        val raw = prefs.getString(KEY_CONNECTIONS, null) ?: return
        val activeKey = prefs.getString(KEY_ACTIVE, null)
        val list = try {
            Json.decodeFromString<List<PersistedConnection>>(raw).map { it.toServerConnection() }
        } catch (_: Exception) {
            emptyList()
        }
        _connections.value = list
        _active.value = list.firstOrNull { it.key() == activeKey } ?: list.firstOrNull()
    }
}
