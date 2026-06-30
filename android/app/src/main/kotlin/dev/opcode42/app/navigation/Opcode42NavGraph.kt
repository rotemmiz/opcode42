package dev.opcode42.app.navigation

import androidx.compose.animation.EnterTransition
import androidx.compose.animation.ExitTransition
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import dev.opcode42.app.ui.AdaptiveChatScreen
import dev.opcode42.feature.chat.DRAFT_SESSION_ID
import dev.opcode42.feature.connections.ui.AddServerScreen
import dev.opcode42.feature.chat.ui.TasksScreen
import dev.opcode42.feature.settings.ui.SettingsScreen
import dev.opcode42.feature.terminal.ui.TerminalScreen
import java.net.URLDecoder
import java.net.URLEncoder

sealed class Screen(val route: String) {
    data object AddServer : Screen("add_server")
    data object Chat : Screen("chat/{sessionId}") {
        fun route(sessionId: String) = "chat/$sessionId"
    }
    /**
     * Lazy "new session" draft — no server session is created until the first prompt is sent.
     * This is also the app's **home**: the multi-pane main page (chat + sessions rail + info
     * panel) is what launches, so the draft sits at the bottom of the back stack and every chat
     * opens on top of it.
     */
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

/**
 * The chat/draft destinations that share [AdaptiveChatScreen]. Switching among them
 * (e.g. picking another session in the multi-pane rail) must swap the chat content in
 * place — not slide a whole new screen over the rail/sidebar — so these transitions are
 * suppressed; transitions to the other destinations (settings, add-server, terminal,
 * tasks) fall through to the NavHost default.
 */
private fun isChatFamily(entry: NavBackStackEntry): Boolean =
    entry.destination.route == Screen.Chat.route || entry.destination.route == Screen.NewChat.route

@Composable
fun Opcode42NavGraph(
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

    // The multi-pane main page is the app's home: it boots straight into the "new session" draft,
    // which hosts the sessions rail (formerly the standalone session-list start screen) alongside
    // the chat and info panes. There is no separate session-list destination.
    NavHost(navController = navController, startDestination = Screen.NewChat.route) {
        composable(Screen.AddServer.route) {
            AddServerScreen(
                onNavigateBack = { navController.popBackStack() },
            )
        }

        composable(
            route = Screen.Chat.route,
            arguments = listOf(navArgument("sessionId") { type = NavType.StringType }),
            enterTransition = { if (isChatFamily(initialState)) EnterTransition.None else null },
            exitTransition = { if (isChatFamily(targetState)) ExitTransition.None else null },
            popEnterTransition = { if (isChatFamily(initialState)) EnterTransition.None else null },
            popExitTransition = { if (isChatFamily(targetState)) ExitTransition.None else null },
        ) { backStackEntry ->
            val sessionId = backStackEntry.arguments?.getString("sessionId") ?: return@composable
            AdaptiveChatScreen(
                sessionId = sessionId,
                onNavigateBack = { navController.popBackStack() },
                onOpenTerminal = { directory ->
                    navController.navigate(Screen.Terminal.route(directory))
                },
                onNavigateToSession = { newSessionId ->
                    // Switch sessions in place: replace the current chat rather than stacking,
                    // so Back returns to the home draft (not a trail of previously-viewed
                    // sessions). Paired with the suppressed chat↔chat transition, this reads as
                    // the same window changing session.
                    navController.navigate(Screen.Chat.route(newSessionId)) {
                        popUpTo(Screen.Chat.route) { inclusive = true }
                        launchSingleTop = true
                    }
                },
                onNewSession = {
                    // Return to the home draft as a fresh composer: pop the open chat (and the old
                    // draft) so a new prompt starts a brand-new session and Back exits the app.
                    navController.navigate(Screen.NewChat.route) {
                        popUpTo(Screen.NewChat.route) { inclusive = true }
                        launchSingleTop = true
                    }
                },
                onOpenSettings = {
                    navController.navigate(Screen.Settings.route)
                },
                onOpenTasksBoard = {
                    navController.navigate(Screen.Tasks.route(sessionId))
                },
            )
        }

        // Lazy "new session" draft: holds no server session. AdaptiveChatScreen runs in draft
        // mode (sessionId == DRAFT_SESSION_ID, injected as a default arg so ChatViewModel's
        // SavedStateHandle resolves to draft mode). As the app's home it stays at the bottom of
        // the back stack: opening a session (or the first prompt creating one) pushes Chat on top,
        // so Back from a chat returns here and Back from here exits the app.
        composable(
            route = Screen.NewChat.route,
            arguments = listOf(
                navArgument("sessionId") {
                    type = NavType.StringType
                    defaultValue = DRAFT_SESSION_ID
                },
            ),
            enterTransition = { if (isChatFamily(initialState)) EnterTransition.None else null },
            exitTransition = { if (isChatFamily(targetState)) ExitTransition.None else null },
            popEnterTransition = { if (isChatFamily(initialState)) EnterTransition.None else null },
            popExitTransition = { if (isChatFamily(targetState)) ExitTransition.None else null },
        ) {
            AdaptiveChatScreen(
                sessionId = DRAFT_SESSION_ID,
                onNavigateBack = { navController.popBackStack() },
                onOpenTerminal = { directory ->
                    navController.navigate(Screen.Terminal.route(directory))
                },
                onNavigateToSession = { newSessionId ->
                    // Keep the draft home beneath: push the chat on top (don't pop the draft) so
                    // Back from the chat returns to this home page rather than exiting the app.
                    navController.navigate(Screen.Chat.route(newSessionId)) {
                        launchSingleTop = true
                    }
                },
                onNewSession = { /* already on a fresh draft */ },
                onOpenSettings = {
                    navController.navigate(Screen.Settings.route)
                },
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
