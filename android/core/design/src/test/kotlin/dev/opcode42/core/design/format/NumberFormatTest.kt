package dev.opcode42.core.design.format

import org.junit.Assert.assertEquals
import org.junit.Test

class NumberFormatTest {

    @Test fun `sub-thousand prints the plain integer`() {
        assertEquals("0", formatCompactCount(0L))
        assertEquals("999", formatCompactCount(999L))
    }

    @Test fun `thousands and millions use one decimal with a dot separator`() {
        assertEquals("1.2K", formatCompactCount(1_234L))
        assertEquals("1.5K", formatCompactCount(1_500L))
        assertEquals("2.4M", formatCompactCount(2_400_000L))
    }

    @Test fun `double overload truncates to a whole count`() {
        assertEquals("1.2K", formatCompactCount(1_234.9))
        assertEquals("42", formatCompactCount(42.0))
    }
}
