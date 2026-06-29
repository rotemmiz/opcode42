package dev.opcode42.feature.sessions

import dev.opcode42.core.model.PermissionRequest
import dev.opcode42.core.model.QuestionRequest
import dev.opcode42.core.model.Session
import dev.opcode42.core.model.SessionTime
import dev.opcode42.core.store.AppState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Unit coverage for [projectSessionList] — the pure store→UI projection that feeds both
 * sessions surfaces: child-hiding, tab-independent counts, filter tabs, search, the
 * per-session status/permission/question maps, and recency-ordered date grouping.
 */
class SessionListProjectionTest {

    private val now = 1_700_000_000_000L

    private fun session(
        id: String,
        archived: Long? = null,
        updated: Long? = null,
        parentID: String? = null,
        title: String? = null,
        directory: String? = null,
    ) = Session(
        id = id,
        title = title,
        directory = directory,
        parentID = parentID,
        time = SessionTime(created = 0, updated = updated, archived = archived),
    )

    private fun project(
        state: AppState,
        showArchived: Boolean = false,
        query: String = "",
        filter: SessionFilter = SessionFilter.All,
    ) = projectSessionList(state.toSessionInputs(), showArchived, query, filter, now)

    private fun SessionListUiState.ids() = groups.flatMap { it.sessions }.map { it.id }

    @Test fun activeSessions_sortedByRecency_archivedExcludedButCounted() {
        val state = AppState(
            sessions = listOf(
                session("a", updated = now - 5_000),
                session("b", updated = now - 1_000),
                session("c", archived = 5, updated = now),
            ),
        )
        val ui = project(state)
        assertEquals(listOf("b", "a"), ui.ids())
        assertEquals(1, ui.archivedCount)
        assertEquals(2, ui.allCount)
    }

    @Test fun childSessions_areHiddenFromTopLevelList() {
        val state = AppState(
            sessions = listOf(
                session("a", updated = now),
                session("child", updated = now, parentID = "a"),
            ),
        )
        val ui = project(state)
        assertEquals(listOf("a"), ui.ids())
        assertEquals(1, ui.allCount)
    }

    @Test fun showArchived_returnsOnlyArchived() {
        val state = AppState(
            sessions = listOf(session("a", updated = now), session("c", archived = 5, updated = now)),
        )
        val ui = project(state, showArchived = true)
        assertEquals(listOf("c"), ui.ids())
        assertTrue(ui.showArchived)
    }

    @Test fun counts_areTabIndependent_andFilterApplies() {
        val perm = PermissionRequest(id = "p1", sessionID = "b", title = "Run rm?")
        val state = AppState(
            sessions = listOf(session("a", updated = now), session("b", updated = now), session("c", updated = now)),
            sessionStatus = mapOf("a" to "busy"),
            permissions = mapOf("b" to listOf(perm)),
        )
        val all = project(state)
        assertEquals(3, all.allCount)
        assertEquals(1, all.workingCount)
        assertEquals(1, all.needsInputCount)

        assertEquals(listOf("a"), project(state, filter = SessionFilter.Working).ids())
        assertEquals(listOf("b"), project(state, filter = SessionFilter.NeedsInput).ids())
        // Counts still reflect the whole active set even under a narrowing filter.
        assertEquals(3, project(state, filter = SessionFilter.Working).allCount)
    }

    @Test fun search_filtersByTitleAndDirectory_caseInsensitive() {
        val state = AppState(
            sessions = listOf(
                session("a", updated = now, title = "Fix Login", directory = "/x/alpha"),
                session("b", updated = now, title = "Refactor", directory = "/x/beta"),
            ),
        )
        assertEquals(listOf("a"), project(state, query = "login").ids())
        assertEquals(listOf("b"), project(state, query = "BETA").ids())
        assertEquals(emptyList<String>(), project(state, query = "zzz").ids())
    }

    @Test fun pendingMaps_surfaceFirstRequestPerSession() {
        val state = AppState(
            sessions = listOf(session("a", updated = now), session("b", updated = now)),
            sessionStatus = mapOf("a" to "busy", "b" to "idle"),
            permissions = mapOf(
                "a" to listOf(
                    PermissionRequest(id = "p1", sessionID = "a"),
                    PermissionRequest(id = "p2", sessionID = "a"),
                ),
            ),
            questions = mapOf("b" to listOf(QuestionRequest(id = "q1", sessionID = "b", message = "Which env?"))),
        )
        val ui = project(state)
        assertEquals("busy", ui.statuses["a"])
        assertEquals("p1", ui.pendingPermissions["a"]?.id)
        assertNull(ui.pendingPermissions["b"])
        assertEquals("q1", ui.pendingQuestions["b"]?.id)
    }

    @Test fun groups_areDateBucketed_recentFirst() {
        val state = AppState(
            sessions = listOf(
                session("today", updated = now),
                session("old", updated = now - 3L * 86_400_000L),
            ),
        )
        val ui = project(state)
        assertEquals(listOf("today", "old"), ui.ids())
        assertEquals("Today", ui.groups.first().header)
        assertEquals(2, ui.groups.size)
    }
}
