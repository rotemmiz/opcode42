package dev.opcode42.feature.settings

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.launch
import javax.inject.Inject
import javax.inject.Singleton

private val Context.dataStore: DataStore<Preferences> by preferencesDataStore(name = "opcode42_prefs")

/**
 * User-selectable theme mode. `System` follows the OS dark setting; the two overrides
 * are persisted so the choice survives process death.
 */
enum class ThemeMode(val storage: String) {
    System("system"),
    Light("light"),
    Dark("dark"),
    ;

    companion object {
        fun fromStorage(value: String?): ThemeMode =
            entries.firstOrNull { it.storage == value } ?: System
    }
}

/**
 * App-wide persisted preferences (DataStore). The light/dark theme is user-selectable
 * (rail footer + Settings); `System` (the default) follows the OS dark setting.
 */
@Singleton
class AppPreferences @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val themeKey = stringPreferencesKey("theme_mode")

    /** Synchronous mirror of the persisted theme mode, for instant cycle-from-current. */
    private val themeModeMirror = MutableStateFlow(ThemeMode.System)

    val themeMode: Flow<ThemeMode> = context.dataStore.data
        .map { prefs -> ThemeMode.fromStorage(prefs[themeKey]) }
        .onEach { themeModeMirror.value = it }

    suspend fun setThemeMode(mode: ThemeMode) {
        context.dataStore.edit { it[themeKey] = mode.storage }
    }

    /**
     * Cycle Dark → Light → System → Dark. Reads the latest observed value (or System if
     * none yet) so the cycle is instant; persists asynchronously via [setThemeMode].
     */
    fun cycleTheme() {
        val next = when (themeModeMirror.value) {
            ThemeMode.Dark -> ThemeMode.Light
            ThemeMode.Light -> ThemeMode.System
            ThemeMode.System -> ThemeMode.Dark
        }
        scope.launch { setThemeMode(next) }
    }

    @Suppress("unused")
    val store: DataStore<Preferences> get() = context.dataStore
}