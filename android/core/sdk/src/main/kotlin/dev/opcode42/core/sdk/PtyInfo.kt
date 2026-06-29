package dev.opcode42.core.sdk

/**
 * Represents a PTY session created via POST /pty.
 */
data class PtyInfo(
    val id: String,
    val title: String? = null,
    val status: String = "running",
)
