package dev.opcode42.feature.sessions

/**
 * Home-prefix shapes for the daemon host's filesystem. The session `directory` is a path on the
 * daemon's machine (macOS/Linux), NOT the Android client — so the JVM's `user.home` never matches.
 * We detect the conventional home roots by shape instead.
 */
private val HOME_PREFIXES = listOf(
    Regex("^/Users/[^/]+"), // macOS
    Regex("^/home/[^/]+"), // Linux
    Regex("^/root"), // root's home
)

/**
 * Abbreviate a daemon-host directory to a `~`-relative path for display:
 * `/Users/rotemmiz/git/opcode42` → `~/git/opcode42`, `/home/bob/x` → `~/x`, `/root/x` → `~/x`.
 * Paths outside a recognized home (`/var/log`) and a bare home root segment (`/Users` with no
 * user) are returned unchanged; a blank/null directory yields `""`.
 */
fun homeRelativeDir(dir: String?): String {
    val d = dir?.takeIf { it.isNotBlank() } ?: return ""
    for (re in HOME_PREFIXES) {
        val m = re.find(d) ?: continue
        if (m.range.first == 0) return "~" + d.substring(m.range.last + 1)
    }
    return d
}
