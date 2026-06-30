package dev.opcode42.feature.chat.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class SplitBudgetsTest {

    @Test fun `both fit — each keeps its full width`() {
        assertEquals(30 to 40, splitBudgets(pathW = 30, branchW = 40, avail = 100))
    }

    @Test fun `exactly fits — no truncation`() {
        assertEquals(50 to 50, splitBudgets(pathW = 50, branchW = 50, avail = 100))
    }

    @Test fun `both too long — split the shortfall about evenly`() {
        // Each is granted half; both ellipsize.
        assertEquals(50 to 50, splitBudgets(pathW = 80, branchW = 90, avail = 100))
    }

    @Test fun `short path — its leftover spills to the long branch`() {
        // Path needs only 20 (< half), so the branch gets the remaining 80 rather than being capped at 50.
        assertEquals(20 to 80, splitBudgets(pathW = 20, branchW = 200, avail = 100))
    }

    @Test fun `short branch — its leftover spills to the long path`() {
        assertEquals(80 to 20, splitBudgets(pathW = 200, branchW = 20, avail = 100))
    }

    @Test fun `odd available width is fully allocated`() {
        val (p, b) = splitBudgets(pathW = 80, branchW = 90, avail = 101)
        assertEquals(101, p + b)
    }
}
