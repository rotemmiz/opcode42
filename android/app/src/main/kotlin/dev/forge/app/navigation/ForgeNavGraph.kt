package dev.forge.app.navigation

import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import dev.forge.feature.connections.ConnectionsViewModel
import dev.forge.feature.connections.ui.AddServerScreen
import dev.forge.feature.chat.ui.ChatScreen
import dev.forge.feature.sessions.ui.SessionListScreen
import dev.forge.feature.settings.ui.SettingsScreen

sealed class Screen(val route: String) {
    data object SessionList : Screen("sessions")
    data object AddServer : Screen("add_server")
    data object Chat : Screen("chat/{sessionId}") {
        fun route(sessionId: String) = "chat/$sessionId"
    }
    data object Settings : Screen("settings")
}

@Composable
fun ForgeNavGraph() {
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
    }
}
