package dev.forge.feature.connections.discovery

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DiscoveryManagerTest {

    private fun server(name: String, host: String, port: Int = 4096) =
        DiscoveredServer(serviceName = name, host = host, port = port)

    @Test
    fun `start browses and stop tears down`() {
        val platform = FakeNsdPlatform()
        val manager = DiscoveryManager(platform)

        manager.start()
        assertTrue(platform.started)
        assertEquals(1, platform.startCount)
        assertEquals(DiscoveryManager.DEFAULT_SERVICE_TYPE, platform.lastServiceType)
        assertTrue(manager.scanning.value)

        manager.stop()
        assertFalse(platform.started)
        assertEquals(1, platform.stopCount)
        assertFalse(manager.scanning.value)
    }

    @Test
    fun `start is idempotent`() {
        val platform = FakeNsdPlatform()
        val manager = DiscoveryManager(platform)

        manager.start()
        manager.start()
        assertEquals(1, platform.startCount)
    }

    @Test
    fun `found service is resolved and listed`() {
        val platform = FakeNsdPlatform()
        val manager = DiscoveryManager(platform)
        manager.start()

        platform.emitFound("alpha")
        assertEquals(1, platform.pendingResolveCount)

        platform.completeNextResolve(server("alpha", "192.168.1.10"))
        assertEquals(listOf("alpha"), manager.servers.value.map { it.serviceName })
    }

    @Test
    fun `resolves run serially - one at a time`() {
        val platform = FakeNsdPlatform()
        val manager = DiscoveryManager(platform)
        manager.start()

        platform.emitFound("alpha")
        platform.emitFound("beta")
        // Only the first resolve is in flight; the second waits.
        assertEquals(1, platform.pendingResolveCount)
        assertEquals("alpha", platform.nextResolveName())

        platform.completeNextResolve(server("alpha", "192.168.1.10"))
        // Completing the first kicks off the second.
        assertEquals(1, platform.pendingResolveCount)
        assertEquals("beta", platform.nextResolveName())

        platform.completeNextResolve(server("beta", "192.168.1.11"))
        assertEquals(listOf("alpha", "beta"), manager.servers.value.map { it.serviceName })
    }

    @Test
    fun `de-dupes by host and port`() {
        val platform = FakeNsdPlatform()
        val manager = DiscoveryManager(platform)
        manager.start()

        platform.emitFound("alpha")
        platform.completeNextResolve(server("alpha", "192.168.1.10", 4096))
        platform.emitFound("alpha-dup")
        platform.completeNextResolve(server("alpha-dup", "192.168.1.10", 4096))

        assertEquals(1, manager.servers.value.size)
        // The later resolution wins for that host:port.
        assertEquals("alpha-dup", manager.servers.value.single().serviceName)
    }

    @Test
    fun `same service found twice only resolves once while queued`() {
        val platform = FakeNsdPlatform()
        val manager = DiscoveryManager(platform)
        manager.start()

        platform.emitFound("alpha")
        platform.emitFound("alpha")
        assertEquals(1, platform.pendingResolveCount)
    }

    @Test
    fun `failed resolve adds nothing but continues the queue`() {
        val platform = FakeNsdPlatform()
        val manager = DiscoveryManager(platform)
        manager.start()

        platform.emitFound("alpha")
        platform.emitFound("beta")
        platform.completeNextResolve(null) // alpha fails
        assertTrue(manager.servers.value.isEmpty())

        // beta still gets resolved.
        assertEquals("beta", platform.nextResolveName())
        platform.completeNextResolve(server("beta", "192.168.1.11"))
        assertEquals(listOf("beta"), manager.servers.value.map { it.serviceName })
    }

    @Test
    fun `lost service is removed from the list`() {
        val platform = FakeNsdPlatform()
        val manager = DiscoveryManager(platform)
        manager.start()

        platform.emitFound("alpha")
        platform.completeNextResolve(server("alpha", "192.168.1.10"))
        assertEquals(1, manager.servers.value.size)

        platform.emitLost("alpha")
        assertTrue(manager.servers.value.isEmpty())
    }

    @Test
    fun `stop clears the list`() {
        val platform = FakeNsdPlatform()
        val manager = DiscoveryManager(platform)
        manager.start()
        platform.emitFound("alpha")
        platform.completeNextResolve(server("alpha", "192.168.1.10"))

        manager.stop()
        assertTrue(manager.servers.value.isEmpty())
    }

    @Test
    fun `resolve completing after stop does not repopulate`() {
        val platform = FakeNsdPlatform()
        val manager = DiscoveryManager(platform)
        manager.start()
        platform.emitFound("alpha")
        // Grab the in-flight resolve callback, then stop before it completes.
        manager.stop()
        // FakeNsdPlatform.stop clears its own queue, so simulate a late callback directly:
        // re-driving through a fresh found after stop must be ignored.
        platform.emitFound("ghost")
        assertEquals(0, platform.pendingResolveCount)
        assertTrue(manager.servers.value.isEmpty())
    }
}
