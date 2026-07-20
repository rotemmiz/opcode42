package dev.opcode42.feature.chat.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.first
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import javax.inject.Inject
import javax.inject.Singleton

private val Context.stashDataStore: DataStore<Preferences> by preferencesDataStore(name = "opcode42_stash")
private val draftsKey = stringPreferencesKey("drafts")
private val json = Json { prettyPrint = false }
private val listSerializer = ListSerializer(String.serializer())

/**
 * Local-only persistence for the `/stash` command: a list of stashed prompt-draft
 * strings the user can save and reload into the composer. No daemon endpoint —
 * drafts live entirely on-device in a DataStore file owned by `:feature:chat`.
 *
 * Pulled behind an interface so the command and sheet are unit-testable with an
 * in-memory fake (no DataStore / Android framework), matching [PushIdentityStore].
 */
interface StashStore {
    suspend fun list(): List<String>
    suspend fun add(draft: String)
    suspend fun delete(index: Int)
}

@Singleton
class DataStoreStashStore @Inject constructor(
    @ApplicationContext private val context: Context,
) : StashStore {
    override suspend fun list(): List<String> {
        val raw = context.stashDataStore.data.first()[draftsKey] ?: return emptyList()
        if (raw.isEmpty()) return emptyList()
        return runCatching { json.decodeFromString(listSerializer, raw) }.getOrDefault(emptyList())
    }

    override suspend fun add(draft: String) {
        val trimmed = draft.trim()
        if (trimmed.isEmpty()) return
        context.stashDataStore.edit { prefs ->
            val current = prefs[draftsKey]?.let {
                runCatching { json.decodeFromString(listSerializer, it) }.getOrDefault(emptyList())
            } ?: emptyList()
            prefs[draftsKey] = json.encodeToString(listSerializer, listOf(trimmed) + current)
        }
    }

    override suspend fun delete(index: Int) {
        context.stashDataStore.edit { prefs ->
            val current = prefs[draftsKey]?.let {
                runCatching { json.decodeFromString(listSerializer, it) }.getOrDefault(emptyList())
            } ?: emptyList()
            if (index !in current.indices) return@edit
            val updated = current.toMutableList().also { it.removeAt(index) }
            if (updated.isEmpty()) {
                prefs.remove(draftsKey)
            } else {
                prefs[draftsKey] = json.encodeToString(listSerializer, updated)
            }
        }
    }
}
