package dev.opcode42.core.store

import dev.opcode42.core.model.AppEvent
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * Holds the reduced [AppState] and serializes mutations through a [Mutex].
 *
 * This is pure multiplatform Kotlin (commonMain) — no Android framework or DI
 * dependencies. On Android it is provided as a `@Singleton` via a Hilt module
 * in androidMain (see StoreModule); a future iOS app constructs it directly.
 */
class AppStore {
    private val _state = MutableStateFlow(AppState())
    val state: StateFlow<AppState> = _state.asStateFlow()

    private val mutex = Mutex()

    suspend fun dispatch(event: AppEvent) = mutex.withLock {
        _state.value = reduce(_state.value, event)
    }

    /** Add an optimistic message before the server confirms it. */
    suspend fun addOptimistic(sessionID: String, text: String): String = mutex.withLock {
        val id = "msg${currentTimeMillis()}_${randomIdSuffix()}"
        val optimistic = OptimisticMessage(id = id, sessionID = sessionID, text = text)
        val current = _state.value
        val list = current.optimisticMessages[sessionID] ?: emptyList()
        _state.value = current.copy(
            optimisticMessages = current.optimisticMessages + (sessionID to list + optimistic)
        )
        id
    }

    /** Remove an optimistic message on failure. */
    suspend fun removeOptimistic(sessionID: String, id: String) = mutex.withLock {
        val current = _state.value
        val list = (current.optimisticMessages[sessionID] ?: emptyList()).filter { it.id != id }
        _state.value = current.copy(
            optimisticMessages = current.optimisticMessages + (sessionID to list)
        )
    }
}

/** Platform wall-clock millis (Android: System.currentTimeMillis). */
internal expect fun currentTimeMillis(): Long

/** Platform-generated short random suffix used to disambiguate optimistic IDs. */
internal expect fun randomIdSuffix(): String
