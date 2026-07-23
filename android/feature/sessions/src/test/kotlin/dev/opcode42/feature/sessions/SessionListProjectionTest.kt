package dev.opcode42.feature.sessions

import dev.opcode42.core.model.PermissionRequest
import dev.opcode42.core.model.QuestionInfo
import dev.opcode42.core.model.QuestionRequest
import dev.opcode42.core.model.Session
import dev.opcode42.core.model.SessionTime
import dev.opcode42.core.store.AppState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
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
    ) = projectSessionList(
        SessionInputs(state.sessions, state.sessionStatus, state.permissions, state.questions),
        showArchived, query, filter, now,
    )

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
            sessionStatus = mapOf("child" to "busy"),
        )
        val ui = project(state)
        assertEquals(listOf("a"), ui.ids())
        assertEquals(1, ui.allCount)
    }

    @Test fun childSessions_areGroupedByParent_inChildrenByParent_neverAtTopLevel() {
        val state = AppState(
            sessions = listOf(
                session("parent", updated = now, title = "Parent"),
                session("c1", updated = now - 1_000, parentID = "parent", title = "Child 1"),
                session("c2", updated = now, parentID = "parent", title = "Child 2"),
                session("orphan", updated = now, parentID = "missing"),
                session("top", updated = now, title = "Top"),
            ),
            // All children busy so they appear in the active-only subtree.
            sessionStatus = mapOf("c1" to "busy", "c2" to "busy", "orphan" to "busy"),
        )
        val ui = project(state)

        // Top-level list keeps only the parents (parentID == null); children never appear.
        assertEquals(listOf("parent", "top"), ui.ids())
        assertEquals(2, ui.allCount)

        // The children map keys only parents that have active children; the orphan's "missing"
        // parent is absent from the top-level list, so it is keyed but never reachable from a
        // row — that's fine (the row looks up children by its own id, which isn't "missing").
        assertEquals(listOf("parent", "missing"), ui.childrenByParent.keys.toList())

        // The parent's children are both present, recency-ordered (newest first).
        val parentChildren = ui.childrenByParent["parent"]
        assertNotNull(parentChildren)
        assertEquals(listOf("c2", "c1"), parentChildren!!.map { it.id })

        // Children don't appear at the top level even when expanded — they're only in the map.
        val allTopLevelIds = ui.groups.flatMap { it.sessions }.map { it.id }.toSet()
        assertFalse("c1" in allTopLevelIds)
        assertFalse("c2" in allTopLevelIds)
        assertFalse("orphan" in allTopLevelIds)

        // A childless parent has no entry in the children map.
        assertNull(ui.childrenByParent["top"])
    }

    @Test fun childSessions_keepBusyArchivedChildren_dropIdleOnes() {
        // "Active" is status-based, not archived-based: a busy archived subagent stays in the
        // subtree; an idle one (archived or not) is filtered out as finished.
        val state = AppState(
            sessions = listOf(
                session("parent", updated = now),
                session("live", updated = now, parentID = "parent"),
                session("gone", updated = now, archived = 5, parentID = "parent"),
            ),
            sessionStatus = mapOf("live" to "busy", "gone" to "idle"),
        )
        val ui = project(state)
        val parentChildren = ui.childrenByParent["parent"]
        assertNotNull(parentChildren)
        assertEquals(listOf("live"), parentChildren!!.map { it.id })
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
        val perm = PermissionRequest(id = "p1", sessionID = "b", permission = "Run rm?")
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
            questions = mapOf("b" to listOf(QuestionRequest(
                id = "q1", sessionID = "b",
                questions = listOf(QuestionInfo(question = "Which env?", header = "Environment")),
            ))),
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

    // ── Per-session subagent dropdown: active-only filtering ───────────────────
    // The dropdown renders `childrenByParent[session.id]`; active-only filtering means only
    // subagents whose status is non-terminal (`busy`/`retry` — anything `isSessionBusy`
    // accepts) appear, and the dropdown collapses entirely when none remain.

    @Test fun subagentDropdown_keepsOnlyBusyChildren_dropsIdle() {
        // 3 subagents: 2 busy, 1 idle → dropdown shows the 2 busy ones, recency-ordered.
        val state = AppState(
            sessions = listOf(
                session("parent", updated = now),
                session("c1", updated = now - 1_000, parentID = "parent"),
                session("c2", updated = now - 2_000, parentID = "parent"),
                session("c3", updated = now, parentID = "parent"),
            ),
            sessionStatus = mapOf("c1" to "busy", "c2" to "busy", "c3" to "idle"),
        )
        val ui = project(state)
        val kids = ui.childrenByParent["parent"]
        assertNotNull(kids)
        assertEquals(setOf("c1", "c2"), kids!!.map { it.id }.toSet())
        // Newest first: c1 (now-1k) before c2 (now-2k).
        assertEquals(listOf("c1", "c2"), kids.map { it.id })
    }

    @Test fun subagentDropdown_emptyWhenAllChildrenIdle() {
        val state = AppState(
            sessions = listOf(
                session("parent", updated = now),
                session("c1", updated = now, parentID = "parent"),
                session("c2", updated = now, parentID = "parent"),
            ),
            sessionStatus = mapOf("c1" to "idle", "c2" to "idle"),
        )
        val ui = project(state)
        // No active children → no entry; the dropdown collapses (no "0 subagents" empty state).
        assertNull(ui.childrenByParent["parent"])
    }

    @Test fun subagentDropdown_emptyWhenNoChildren() {
        val state = AppState(sessions = listOf(session("parent", updated = now)))
        val ui = project(state)
        assertNull(ui.childrenByParent["parent"])
    }

    @Test fun subagentDropdown_dropsChildWhenItTransitionsBusyToIdle() {
        // Same parent+child; first busy (present), then idle (gone) — modelled as two states.
        val base = AppState(
            sessions = listOf(
                session("parent", updated = now),
                session("c1", updated = now, parentID = "parent"),
            ),
        )
        val busy = project(base.copy(sessionStatus = mapOf("c1" to "busy")))
        assertEquals(listOf("c1"), busy.childrenByParent["parent"]!!.map { it.id })
        val idle = project(base.copy(sessionStatus = mapOf("c1" to "idle")))
        assertNull(idle.childrenByParent["parent"])
    }

    @Test fun subagentDropdown_addsChildWhenItTransitionsIdleToBusy() {
        // A newly-spawned subagent reports busy once it starts running → it appears.
        val base = AppState(
            sessions = listOf(
                session("parent", updated = now),
                session("c1", updated = now, parentID = "parent"),
            ),
        )
        assertNull(project(base).childrenByParent["parent"]) // no status yet → not active
        val ui = project(base.copy(sessionStatus = mapOf("c1" to "busy")))
        assertEquals(listOf("c1"), ui.childrenByParent["parent"]!!.map { it.id })
    }

    @Test fun subagentDropdown_retryCountsAsActive() {
        // `retry` is the other non-terminal SessionStatus.type — a retrying subagent is active.
        val state = AppState(
            sessions = listOf(
                session("parent", updated = now),
                session("c1", updated = now, parentID = "parent"),
            ),
            sessionStatus = mapOf("c1" to "retry"),
        )
        val ui = project(state)
        assertEquals(listOf("c1"), ui.childrenByParent["parent"]!!.map { it.id })
    }

    @Test fun projectActiveSubagents_isPureAndSortsRecency() {
        // Direct unit test of the pure function: independent of the full projection.
        val sessions = listOf(
            session("c1", updated = 100, parentID = "p"),
            session("c2", updated = 300, parentID = "p"),
            session("c3", updated = 200, parentID = "p"),
            session("top", updated = 999), // no parentID — ignored
        )
        val map = projectActiveSubagents(sessions, mapOf("c1" to "busy", "c3" to "retry"))
        assertEquals(listOf("c3", "c1"), map["p"]!!.map { it.id })
        assertNull(map["top"])
    }
}
