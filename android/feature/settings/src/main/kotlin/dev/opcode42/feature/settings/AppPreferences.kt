package dev.opcode42.feature.settings

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.preferencesDataStore
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton

private val Context.dataStore: DataStore<Preferences> by preferencesDataStore(name = "opcode42_prefs")

/**
 * App-wide persisted preferences (DataStore). Reserved for future settings — the
 * light/dark theme now follows the OS (`isSystemInDarkTheme()`), so there is no
 * theme preference stored here. [store] is the entry point for new preferences.
 */
@Singleton
class AppPreferences @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    @Suppress("unused")
    val store: DataStore<Preferences> get() = context.dataStore
}
