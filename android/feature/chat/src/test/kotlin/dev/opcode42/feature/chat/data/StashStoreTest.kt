package dev.opcode42.feature.chat.data

import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pins the [StashStore] contract: list/add/delete round-trip, newest-first ordering,
 * blank-draft rejection, and out-of-range delete being a no-op. Runs against an
 * in-memory fake (the DataStore implementation needs an Android `Context` and is
 * exercised on-device), matching the `PushRegistrarTest` → `FakeIdentityStore` pattern.
 */
class StashStoreTest {

    @Test
    fun startsEmpty() = runTest {
        val store = FakeStashStore()
        assertEquals(emptyList<String>(), store.list())
    }

    @Test
    fun addPersistsAndListsNewestFirst() = runTest {
        val store = FakeStashStore()
        store.add("draft one")
        store.add("draft two")
        assertEquals(listOf("draft two", "draft one"), store.list())
    }

    @Test
    fun addTrimsAndRejectsBlank() = runTest {
        val store = FakeStashStore()
        store.add("  trimmed  ")
        store.add("")
        store.add("   ")
        assertEquals(listOf("trimmed"), store.list())
    }

    @Test
    fun deleteRemovesByIndex() = runTest {
        val store = FakeStashStore()
        store.add("a")
        store.add("b")
        store.add("c")
        store.delete(0)
        assertEquals(listOf("b", "a"), store.list())
    }

    @Test
    fun deleteOutOfRangeIsNoOp() = runTest {
        val store = FakeStashStore()
        store.add("a")
        store.delete(-1)
        store.delete(5)
        assertEquals(listOf("a"), store.list())
    }

    @Test
    fun deleteOnEmptyIsNoOp() = runTest {
        val store = FakeStashStore()
        store.delete(0)
        assertTrue(store.list().isEmpty())
    }
}

/** In-memory [StashStore] mirroring the DataStore implementation's semantics. */
class FakeStashStore : StashStore {
    private val drafts = mutableListOf<String>()

    override suspend fun list(): List<String> = drafts.toList()
    override suspend fun add(draft: String) {
        val trimmed = draft.trim()
        if (trimmed.isNotEmpty()) drafts.add(0, trimmed)
    }
    override suspend fun delete(index: Int) {
        if (index in drafts.indices) drafts.removeAt(index)
    }
}
