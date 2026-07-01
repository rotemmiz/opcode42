package dev.opcode42.core.store

import dev.opcode42.core.model.*

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
    is AppEvent.SessionStatus -> state.copy(
        sessionStatus = state.sessionStatus + (event.sessionId to event.status)
    )

    is AppEvent.MessageUpdated -> {
        val sessionMessages = state.messages[event.message.sessionID] ?: emptyList()
        // SSE `message.updated` events carry only metadata (role, modelID, etc.) and have
        // no `parts` field — the parts arrive separately via `message.part.updated`. Preserve
        // the existing parts so REST-loaded content is not wiped by metadata-only SSE events.
        val incoming = if (event.message.parts.isEmpty()) {
            val existing = sessionMessages.firstOrNull { it.id == event.message.id }
            if (existing != null && existing.parts.isNotEmpty()) event.message.copy(parts = existing.parts)
            else event.message
        } else event.message
        val updated = sessionMessages.upsertById(incoming) { it.id }
        // Remove optimistic entry when the server echoes back the user message
        val optimistic = state.optimisticMessages[event.message.sessionID] ?: emptyList()
        val confirmedOptimistic = if (event.message.role == "user") emptyList() else optimistic
        state.copy(
            messages = state.messages + (event.message.sessionID to updated),
            optimisticMessages = if (confirmedOptimistic.size != optimistic.size)
                state.optimisticMessages + (event.message.sessionID to confirmedOptimistic)
            else state.optimisticMessages,
        )
    }
    is AppEvent.MessageRemoved -> {
        // The event carries sessionID — index straight in; scan only if it's blank (legacy wire).
        val sessionID = event.sessionId.takeIf { state.messages.containsKey(it) }
            ?: state.messages.entries.firstOrNull { (_, msgs) -> msgs.any { it.id == event.messageId } }?.key
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
        val messageID = state.messageIdForPart(event.messageId, event.partId) ?: return state
        state.copy(
            parts = state.parts + (messageID to
                (state.parts[messageID] ?: emptyList()).filter { it.id != event.partId })
        )
    }
    is AppEvent.PartDelta -> {
        // Append delta text to the TextPart identified by partId. This is the hottest event on
        // the stream, so index straight into the message-keyed parts bucket via the carried
        // messageId; the full scan is a fallback only when messageId is blank/unknown.
        val messageID = state.messageIdForPart(event.messageId, event.partId) ?: return state
        val updated = (state.parts[messageID] ?: emptyList()).map { part ->
            if (part.id == event.partId && part is TextPart)
                part.copy(text = part.text + event.delta)
            else part
        }
        state.copy(parts = state.parts + (messageID to updated))
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

    is AppEvent.SessionDiffLoaded -> state.copy(
        diffs = state.diffs + (event.messageId to event.diffs)
    )

    is AppEvent.Unknown -> state
}

/**
 * Resolve the message bucket holding [partId]. Prefers the [messageId] carried on the event —
 * an O(1) map lookup — and only falls back to an O(total-parts) scan when that id is blank or
 * the bucket doesn't exist yet (e.g. a delta racing ahead of its `message.part.updated`).
 */
private fun AppState.messageIdForPart(messageId: String, partId: String): String? =
    messageId.takeIf { parts.containsKey(it) }
        ?: parts.entries.firstOrNull { (_, ps) -> ps.any { it.id == partId } }?.key

// ─── Sorted list helpers ───────────────────────────────────────────────────────

/** Binary-search upsert into a list sorted by String key (lexicographic). */
private fun <T> List<T>.upsertById(item: T, key: (T) -> String): List<T> {
    val k = key(item)
    var lo = 0; var hi = size - 1
    while (lo <= hi) {
        val mid = (lo + hi) ushr 1
        val midKey = key(this[mid])
        when {
            midKey == k -> return toMutableList().also { it[mid] = item }
            midKey < k -> lo = mid + 1
            else -> hi = mid - 1
        }
    }
    // lo is the sorted insertion point
    return toMutableList().also { it.add(lo, item) }
}
