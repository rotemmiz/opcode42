package dev.opcode42.feature.chat.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class NormalizeRmsTest {
    @Test
    fun `floor and below clamp to zero`() {
        assertEquals(0f, normalizeRms(-2f), 0f)
        assertEquals(0f, normalizeRms(-20f), 0f)
    }

    @Test
    fun `ceiling and above clamp to one`() {
        assertEquals(1f, normalizeRms(10f), 0f)
        assertEquals(1f, normalizeRms(50f), 0f)
    }

    @Test
    fun `midpoint maps to one half`() {
        // floor -2, ceil 10 → midpoint 4 dB
        assertEquals(0.5f, normalizeRms(4f), 1e-4f)
    }

    @Test
    fun `output is monotonic in input`() {
        assertTrue(normalizeRms(0f) < normalizeRms(5f))
        assertTrue(normalizeRms(5f) < normalizeRms(9f))
    }

    @Test
    fun `amplitude attack rises faster than release falls`() {
        // From an equal gap of 0.5, the rising step should cover more ground than
        // the falling one (fast attack, slow release).
        val rise = nextAmplitude(0f, 0.5f) - 0f
        val fall = 0.5f - nextAmplitude(0.5f, 0f)
        assertTrue(rise > fall)
    }

    @Test
    fun `amplitude steps toward target without overshoot`() {
        // Rising: between previous and target.
        val up = nextAmplitude(0f, 1f)
        assertTrue(up > 0f && up < 1f)
        // Falling: between target and previous.
        val down = nextAmplitude(1f, 0f)
        assertTrue(down in 0f..1f && down < 1f)
    }

    @Test
    fun `amplitude is a fixed point when already at target`() {
        assertEquals(0.5f, nextAmplitude(0.5f, 0.5f), 1e-6f)
    }
}
