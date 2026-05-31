package dev.forge.feature.connections.discovery

/**
 * A daemon discovered on the local network over mDNS (`_opencode._tcp.`), after resolution.
 *
 * Pure data + derived accessors so the parsing/URL logic is unit-testable without Android types.
 * See plan 07 — "mDNS / LAN auto-discovery". TXT keys: path / directory / version / auth.
 */
data class DiscoveredServer(
    val serviceName: String,
    val host: String,
    val port: Int,
    val txt: Map<String, String> = emptyMap(),
) {
    /** Base path advertised by the daemon (default "/"). */
    val path: String get() = txt["path"]?.takeIf { it.isNotBlank() } ?: "/"

    /** Suggested `x-opencode-directory`, if advertised. */
    val directory: String? get() = txt["directory"]?.takeIf { it.isNotBlank() }

    /** Daemon version string, if advertised (display + compat hint). */
    val version: String? get() = txt["version"]?.takeIf { it.isNotBlank() }

    /** Auth scheme the daemon expects — drives the auth form. */
    val authType: AuthType get() = AuthType.fromTxt(txt["auth"])

    /**
     * `scheme://host:port[/base-path]` — fed straight into [normalizeServerUrl], the same path as
     * a manual add. Bracketed for IPv6 literals.
     */
    val url: String
        get() {
            val hostPart = if (host.contains(':') && !host.startsWith("[")) "[$host]" else host
            val base = "http://$hostPart:$port"
            val trimmed = path.trim().trimEnd('/')
            return when {
                trimmed.isEmpty() -> base
                trimmed.startsWith("/") -> base + trimmed
                else -> "$base/$trimmed"
            }
        }
}

/** Auth scheme advertised via the `auth` TXT key. */
enum class AuthType {
    NONE, BASIC, TOKEN, UNKNOWN;

    companion object {
        fun fromTxt(value: String?): AuthType = when (value?.trim()?.lowercase()) {
            "none" -> NONE
            "basic" -> BASIC
            "token" -> TOKEN
            else -> UNKNOWN
        }
    }
}

/**
 * Decode an mDNS TXT-record attribute map (raw UTF-8 bytes, as exposed by
 * `NsdServiceInfo.getAttributes()`) into plain strings. Null values become empty strings
 * (a key present with no value, e.g. a bare flag).
 */
fun parseTxtAttributes(attributes: Map<String, ByteArray?>): Map<String, String> =
    attributes.entries
        .mapNotNull { (key, value) ->
            if (key.isNullOrBlank()) null
            else key to (value?.toString(Charsets.UTF_8) ?: "")
        }
        .toMap()
