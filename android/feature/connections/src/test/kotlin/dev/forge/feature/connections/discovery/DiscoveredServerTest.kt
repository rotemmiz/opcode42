package dev.forge.feature.connections.discovery

import org.junit.Assert.assertEquals
import org.junit.Test

class DiscoveredServerTest {

    @Test
    fun `url has no path segment when path is root`() {
        val server = DiscoveredServer("opencode", "192.168.1.10", 4096, mapOf("path" to "/"))
        assertEquals("http://192.168.1.10:4096", server.url)
    }

    @Test
    fun `url defaults to root path when path txt absent`() {
        val server = DiscoveredServer("opencode", "192.168.1.10", 4096)
        assertEquals("http://192.168.1.10:4096", server.url)
        assertEquals("/", server.path)
    }

    @Test
    fun `url appends a non-root base path`() {
        val server = DiscoveredServer("opencode", "host.local", 80, mapOf("path" to "/api/"))
        assertEquals("http://host.local:80/api", server.url)
    }

    @Test
    fun `url adds leading slash to a bare path`() {
        val server = DiscoveredServer("opencode", "host.local", 80, mapOf("path" to "api"))
        assertEquals("http://host.local:80/api", server.url)
    }

    @Test
    fun `url brackets an ipv6 literal host`() {
        val server = DiscoveredServer("opencode", "fe80::1", 4096)
        assertEquals("http://[fe80::1]:4096", server.url)
    }

    @Test
    fun `auth type parses known schemes case-insensitively`() {
        assertEquals(AuthType.NONE, DiscoveredServer("s", "h", 1, mapOf("auth" to "none")).authType)
        assertEquals(AuthType.BASIC, DiscoveredServer("s", "h", 1, mapOf("auth" to "Basic")).authType)
        assertEquals(AuthType.TOKEN, DiscoveredServer("s", "h", 1, mapOf("auth" to "TOKEN")).authType)
    }

    @Test
    fun `auth type is unknown when absent or unrecognized`() {
        assertEquals(AuthType.UNKNOWN, DiscoveredServer("s", "h", 1).authType)
        assertEquals(AuthType.UNKNOWN, DiscoveredServer("s", "h", 1, mapOf("auth" to "weird")).authType)
    }

    @Test
    fun `directory and version read from txt`() {
        val server = DiscoveredServer(
            "opencode", "h", 1,
            mapOf("directory" to "/work/proj", "version" to "0.5.1"),
        )
        assertEquals("/work/proj", server.directory)
        assertEquals("0.5.1", server.version)
    }

    @Test
    fun `parseTxtAttributes decodes utf8 and drops blank keys`() {
        val attrs = mapOf(
            "path" to "/".toByteArray(),
            "version" to "dev".toByteArray(),
            "flag" to null,
            "" to "ignored".toByteArray(),
        )
        val parsed = parseTxtAttributes(attrs)
        assertEquals("/", parsed["path"])
        assertEquals("dev", parsed["version"])
        assertEquals("", parsed["flag"])
        assertEquals(3, parsed.size)
    }
}
