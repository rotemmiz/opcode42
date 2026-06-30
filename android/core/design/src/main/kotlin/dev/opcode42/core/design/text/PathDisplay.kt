package dev.opcode42.core.design.text

private val HOME_PREFIX = Regex("""^(?:/Users/[^/]+|/home/[^/]+|/root)(?=/|$)""")

/**
 * Abbreviate a daemon-host directory to a `~`-relative path for display:
 * `/Users/rotemmiz/git/opcode42` → `~/git/opcode42`, `/home/bob/x` → `~/x`, `/root/x` → `~/x`.
 *
 * The session `directory` is a path on the DAEMON's machine (macOS/Linux), not the Android client,
 * so the JVM's `user.home` never matches — the conventional home roots are detected by shape,
 * whole-segment only (so `/rootkit` and `/homestead` are left intact). Paths outside a recognized
 * home are returned unchanged; a blank/null directory yields `""`. Used by the chat header subtitle
 * and the sessions-rail footer so both render the same path identically.
 */
fun homeRelativeDir(dir: String?): String {
    val d = dir?.takeIf { it.isNotBlank() } ?: return ""
    return HOME_PREFIX.replaceFirst(d, "~")
}
