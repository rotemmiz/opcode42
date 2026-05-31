package dev.forge.app.navigation

import androidx.compose.runtime.Composable
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import dev.forge.feature.connections.ui.AddServerScreen
import dev.forge.feature.chat.ui.ChatScreen
import dev.forge.feature.sessions.ui.SessionListScreen
import dev.forge.feature.settings.ui.SettingsScreen
import dev.forge.feature.terminal.ui.TerminalScreen
import java.net.URLDecoder
import java.net.URLEncoder

sealed class Screen(val route: String) {
    data object SessionList : Screen("sessions")
    data object AddServer : Screen("add_server")
    data object Chat : Screen("chat/{sessionId}") {
        fun route(sessionId: String) = "chat/$sessionId"
    }
    data object Settings : Screen("settings")
    data object Terminal : Screen("terminal/{directory}") {
        fun route(directory: String) =
            "terminal/${URLEncoder.encode(directory, "UTF-8")}"
    }
}

@Composable
fun ForgeNavGraph(
    isDarkTheme: Boolean = true,
    onToggleTheme: () -> Unit = {},
) {
    val navController = rememberNavController()

    NavHost(navController = navController, startDestination = Screen.SessionList.route) {
        composable(Screen.SessionList.route) {
            SessionListScreen(
                onSessionClick = { session ->
                    navController.navigate(Screen.Chat.route(session.id))
                },
                onAddServerClick = {
                    navController.navigate(Screen.AddServer.route)
                },
                onSettingsClick = {
                    navController.navigate(Screen.Settings.route)
                },
            )
        }

        composable(Screen.AddServer.route) {
            AddServerScreen(
                onNavigateBack = { navController.popBackStack() },
            )
        }

        composable(
            route = Screen.Chat.route,
            arguments = listOf(navArgument("sessionId") { type = NavType.StringType }),
        ) { backStackEntry ->
            val sessionId = backStackEntry.arguments?.getString("sessionId") ?: return@composable
            ChatScreen(
                sessionId = sessionId,
                onNavigateBack = { navController.popBackStack() },
                onOpenTerminal = { directory ->
                    navController.navigate(Screen.Terminal.route(directory))
                },
                onNavigateToSession = { newSessionId ->
                    navController.navigate(Screen.Chat.route(newSessionId))
                },
                isDarkTheme = isDarkTheme,
                onToggleTheme = onToggleTheme,
            )
        }

        composable(Screen.Settings.route) {
            SettingsScreen(
                onNavigateBack = { navController.popBackStack() },
                onAddServer = {
                    navController.navigate(Screen.AddServer.route)
                },
            )
        }

        composable(
            route = Screen.Terminal.route,
            arguments = listOf(navArgument("directory") { type = NavType.StringType }),
        ) { backStackEntry ->
            val encodedDir = backStackEntry.arguments?.getString("directory") ?: return@composable
            val directory = URLDecoder.decode(encodedDir, "UTF-8")
            TerminalScreen(
                directory = directory,
                onBack = { navController.popBackStack() },
            )
        }
    }
}
