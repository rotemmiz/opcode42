package dev.opcode42.core.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * VCS branch info for a directory, as reported by `GET /vcs?directory=<dir>`. Both fields
 * are optional: a non-repo directory, a detached HEAD, or a backend that doesn't implement
 * `/vcs` (the Go daemon currently 501s) yields an empty object, so callers must tolerate nulls.
 */
@Serializable
data class VcsInfo(
    val branch: String? = null,
    @SerialName("default_branch") val defaultBranch: String? = null,
)
