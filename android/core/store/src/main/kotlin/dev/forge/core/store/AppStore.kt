package dev.forge.core.store

import dev.forge.core.model.AppEvent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class AppStore @Inject constructor() {
    private val _state = MutableStateFlow(AppState())
    val state: StateFlow<AppState> = _state.asStateFlow()

    private val mutex = Mutex()

    suspend fun dispatch(event: AppEvent) = mutex.withLock {
        _state.value = reduce(_state.value, event)
    }

    /** Add an optimistic message before the server confirms it. */
    suspend fun addOptimistic(sessionID: String, text: String): String = mutex.withLock {
        val id = "msg${System.currentTimeMillis()}_${UUID.randomUUID().toString().take(8)}"
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
