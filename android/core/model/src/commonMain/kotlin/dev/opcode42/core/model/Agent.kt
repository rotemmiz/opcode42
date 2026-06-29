package dev.opcode42.core.model

import kotlinx.serialization.Serializable

/**
 * An agent surfaced by GET /agent. `mode` is subagent | primary | all; only primary/all
 * agents are selectable as the session's main agent (subagents are spawned by the agent itself).
 */
@Serializable
data class AgentInfo(
    val name: String,
    val description: String? = null,
    val mode: String? = null,
    val model: ModelRef? = null,
) {
    /** Selectable as the session's main agent (not a subagent-only helper). */
    val isPrimary: Boolean get() = mode == "primary" || mode == "all" || mode == null
}
