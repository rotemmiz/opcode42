package dev.opcode42.app.navigation

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import dev.opcode42.app.ui.AdaptiveChatScreen
import dev.opcode42.feature.chat.DRAFT_SESSION_ID
import dev.opcode42.feature.connections.ui.AddServerScreen
import dev.opcode42.feature.chat.ui.TasksScreen
import dev.opcode42.feature.sessions.ui.SessionListScreen
import dev.opcode42.feature.settings.ui.SettingsScreen
import dev.opcode42.feature.terminal.ui.TerminalScreen
import java.net.URLDecoder
import java.net.URLEncoder

sealed class Screen(val route: String) {
    data object SessionList : Screen("sessions")
    data object AddServer : Screen("add_server")
    data object Chat : Screen("chat/{sessionId}") {
        fun route(sessionId: String) = "chat/$sessionId"
    }
    /** Lazy "new session" draft — no server session is created until the first prompt is sent. */
    data object NewChat : Screen("chat_new")
    data object Settings : Screen("settings")
    data object Terminal : Screen("terminal/{directory}") {
        fun route(directory: String) =
            "terminal/${URLEncoder.encode(directory, "UTF-8")}"
    }
    data object Tasks : Screen("tasks/{sessionId}") {
        fun route(sessionId: String) = "tasks/$sessionId"
    }
}

@Composable
fun Opcode42NavGraph(
    isDarkTheme: Boolean = true,
    onToggleTheme: () -> Unit = {},
    deepLinkSessionId: String? = null,
    deepLinkToken: Long = -1L,
    onDeepLinkConsumed: () -> Unit = {},
) {
    val navController = rememberNavController()

    // A push-notification tap deep-links straight to the relevant Chat screen.
    // Keyed on the tap token (not the session id) so a repeat push for the same
    // session still re-navigates.
    LaunchedEffect(deepLinkToken) {
        val sessionId = deepLinkSessionId ?: return@LaunchedEffect
        navController.navigate(Screen.Chat.route(sessionId)) {
            launchSingleTop = true
        }
        onDeepLinkConsumed()
    }

    NavHost(navController = navController, startDestination = Screen.SessionList.route) {
        composable(Screen.SessionList.route) {
            SessionListScreen(
                onSessionClick = { session ->
                    navController.navigate(Screen.Chat.route(session.id))
                },
                onNewSession = {
                    navController.navigate(Screen.NewChat.route)
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
            AdaptiveChatScreen(
                sessionId = sessionId,
                isDarkTheme = isDarkTheme,
                onToggleTheme = onToggleTheme,
                onNavigateBack = { navController.popBackStack() },
                onOpenTerminal = { directory ->
                    navController.navigate(Screen.Terminal.route(directory))
                },
                onNavigateToSession = { newSessionId ->
                    navController.navigate(Screen.Chat.route(newSessionId))
                },
                onNewSession = {
                    navController.navigate(Screen.NewChat.route)
                },
                onOpenTasksBoard = {
                    navController.navigate(Screen.Tasks.route(sessionId))
                },
            )
        }

        // Lazy "new session" draft: holds no server session. AdaptiveChatScreen runs in draft
        // mode (sessionId == DRAFT_SESSION_ID, injected as a default arg so ChatViewModel's
        // SavedStateHandle resolves to draft mode). The first prompt creates the real session
        // and navigates here → Chat, popping the draft so Back never lands on an empty composer.
        composable(
            route = Screen.NewChat.route,
            arguments = listOf(
                navArgument("sessionId") {
                    type = NavType.StringType
                    defaultValue = DRAFT_SESSION_ID
                },
            ),
        ) {
            AdaptiveChatScreen(
                sessionId = DRAFT_SESSION_ID,
                isDarkTheme = isDarkTheme,
                onToggleTheme = onToggleTheme,
                onNavigateBack = { navController.popBackStack() },
                onOpenTerminal = { directory ->
                    navController.navigate(Screen.Terminal.route(directory))
                },
                onNavigateToSession = { newSessionId ->
                    navController.navigate(Screen.Chat.route(newSessionId)) {
                        popUpTo(Screen.NewChat.route) { inclusive = true }
                        launchSingleTop = true
                    }
                },
                onNewSession = { /* already on a fresh draft */ },
                onOpenTasksBoard = { /* no tasks board until a session exists */ },
            )
        }

        composable(
            route = Screen.Tasks.route,
            arguments = listOf(navArgument("sessionId") { type = NavType.StringType }),
        ) {
            TasksScreen(onNavigateBack = { navController.popBackStack() })
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
