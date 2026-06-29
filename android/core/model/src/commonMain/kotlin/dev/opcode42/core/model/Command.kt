package dev.opcode42.core.model

import kotlinx.serialization.Serializable

/** A slash command surfaced by GET /command (source: command | mcp | skill). */
@Serializable
data class CommandInfo(
    val name: String,
    val description: String? = null,
    val agent: String? = null,
    val model: String? = null,
    val source: String? = null,
    val template: String? = null,
    val subtask: Boolean? = null,
    val hints: List<String> = emptyList(),
)
