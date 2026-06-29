package dev.opcode42.core.model

import kotlinx.serialization.Serializable

/**
 * A project as reported by `GET /project`. opencode groups sessions under projects; each
 * project has a primary [worktree] plus zero or more [sandboxes] (extra worktrees/dirs).
 * The session list aggregator queries `GET /session?directory=<dir>` for every worktree +
 * sandbox to assemble a global, cross-directory list without requiring a configured folder.
 */
@Serializable
data class Project(
    val id: String,
    val worktree: String? = null,
    val sandboxes: List<String> = emptyList(),
    val vcs: String? = null,
    val time: ProjectTime? = null,
)

@Serializable
data class ProjectTime(
    val created: Long = 0,
    val updated: Long? = null,
)
