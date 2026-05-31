package dev.forge.core.store

import dev.forge.core.model.*

/**
 * Pure function — mirrors applyGlobalEvent + applyDirectoryEvent from
 * opencode's packages/app/src/context/global-sync/event-reducer.ts.
 *
 * All list mutations use binary search on lexicographically-sorted IDs to
 * mirror Binary.search in the opencode event reducer.
 */
fun reduce(state: AppState, event: AppEvent): AppState = when (event) {
    is AppEvent.ServerConnected -> state.copy(connectionState = ConnectionState.Connected)
    is AppEvent.ServerHeartbeat -> state
    is AppEvent.GlobalDisposed -> AppState(connectionState = ConnectionState.Disconnected)

    is AppEvent.SessionUpdated -> state.copy(
        sessions = state.sessions.upsertById(event.session) { it.id }
    )
    is AppEvent.SessionRemoved -> state.copy(
        sessions = state.sessions.filter { it.id != event.sessionId }
    )

    is AppEvent.MessageUpdated -> {
        val sessionMessages = state.messages[event.message.sessionID] ?: emptyList()
        val updated = sessionMessages.upsertById(event.message) { it.id }
        // Confirm matching optimistic message
        val optimistic = state.optimisticMessages[event.message.sessionID] ?: emptyList()
        val confirmedOptimistic = optimistic.filter { it.id != event.message.id }
        state.copy(
            messages = state.messages + (event.message.sessionID to updated),
            optimisticMessages = if (confirmedOptimistic.size != optimistic.size)
                state.optimisticMessages + (event.message.sessionID to confirmedOptimistic)
            else state.optimisticMessages,
        )
    }
    is AppEvent.MessageRemoved -> {
        val sessionID = state.messages.entries
            .firstOrNull { (_, msgs) -> msgs.any { it.id == event.messageId } }?.key
            ?: return state
        state.copy(
            messages = state.messages + (sessionID to
                (state.messages[sessionID] ?: emptyList()).filter { it.id != event.messageId })
        )
    }

    is AppEvent.PartUpdated -> {
        val msgParts = state.parts[event.part.messageID] ?: emptyList()
        state.copy(
            parts = state.parts + (event.part.messageID to msgParts.upsertById(event.part) { it.id })
        )
    }
    is AppEvent.PartRemoved -> {
        val entry = state.parts.entries
            .firstOrNull { (_, parts) -> parts.any { it.id == event.partId } }
            ?: return state
        state.copy(
            parts = state.parts + (entry.key to entry.value.filter { it.id != event.partId })
        )
    }
    is AppEvent.PartDelta -> {
        // Append delta text to the TextPart identified by partId
        val entry = state.parts.entries
            .firstOrNull { (_, parts) -> parts.any { it.id == event.partId } }
            ?: return state
        val updated = entry.value.map { part ->
            if (part.id == event.partId && part is TextPart)
                part.copy(text = part.text + event.delta)
            else part
        }
        state.copy(parts = state.parts + (entry.key to updated))
    }

    is AppEvent.PermissionAsked -> {
        val list = state.permissions[event.permission.sessionID] ?: emptyList()
        state.copy(
            permissions = state.permissions + (event.permission.sessionID to list + event.permission)
        )
    }
    is AppEvent.PermissionReplied -> state.copy(
        permissions = state.permissions.mapValues { (_, reqs) ->
            reqs.filter { it.id != event.requestId }
        }
    )

    is AppEvent.QuestionAsked -> {
        val list = state.questions[event.question.sessionID] ?: emptyList()
        state.copy(
            questions = state.questions + (event.question.sessionID to list + event.question)
        )
    }
    is AppEvent.QuestionReplied, is AppEvent.QuestionRejected -> {
        val id = when (event) {
            is AppEvent.QuestionReplied -> event.requestId
            is AppEvent.QuestionRejected -> event.requestId
            else -> return state
        }
        state.copy(
            questions = state.questions.mapValues { (_, qs) -> qs.filter { it.id != id } }
        )
    }

    is AppEvent.Unknown -> state
}

// ─── Sorted list helpers ───────────────────────────────────────────────────────

/** Binary-search upsert into a list sorted by String key (lexicographic). */
private fun <T> List<T>.upsertById(item: T, key: (T) -> String): List<T> {
    val k = key(item)
    val idx = indexOfFirst { key(it) == k }
    return if (idx >= 0) {
        // Replace existing
        toMutableList().also { it[idx] = item }
    } else {
        // Insert in sorted position
        val insertAt = indexOfFirst { key(it) > k }.let { if (it < 0) size else it }
        toMutableList().also { it.add(insertAt, item) }
    }
}
