package dev.opcode42.core.design.theme

import android.os.Build
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DynamicColorGateTest {

    @Test fun offByDefaultEvenOnApi31() {
        assertFalse(shouldUseDynamicColor(dynamicColor = false, sdkInt = 31))
    }

    @Test fun enabledOnApi31UsesDynamicColor() {
        assertTrue(shouldUseDynamicColor(dynamicColor = true, sdkInt = 31))
    }

    @Test fun enabledAbove31UsesDynamicColor() {
        assertTrue(shouldUseDynamicColor(dynamicColor = true, sdkInt = 33))
    }

    @Test fun enabledBelow31FallsBackToBrandPalette() {
        assertFalse(shouldUseDynamicColor(dynamicColor = true, sdkInt = 30))
    }

    @Test fun realSdkConstantMatchesGate() {
        val expected = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S
        assertEquals(expected, shouldUseDynamicColor(dynamicColor = true))
    }
}
