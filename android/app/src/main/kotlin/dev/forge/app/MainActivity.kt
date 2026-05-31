package dev.forge.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import dagger.hilt.android.AndroidEntryPoint
import dev.forge.app.navigation.ForgeNavGraph
import dev.forge.feature.chat.ui.ForgeTheme
import dev.forge.feature.settings.AppPreferences
import kotlinx.coroutines.launch
import javax.inject.Inject

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    @Inject
    lateinit var prefs: AppPreferences

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            val darkTheme by prefs.darkTheme.collectAsState(initial = true)
            val scope = rememberCoroutineScope()
            ForgeTheme(darkTheme = darkTheme) {
                ForgeNavGraph(
                    isDarkTheme = darkTheme,
                    onToggleTheme = { scope.launch { prefs.setDarkTheme(!darkTheme) } },
                )
            }
        }
    }
}
