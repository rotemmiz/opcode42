package dev.opcode42.core.model

import kotlinx.serialization.Serializable

@Serializable
data class SnapshotFileDiff(
    val file: String? = null,
    val patch: String? = null,
    val additions: Int = 0,
    val deletions: Int = 0,
    val status: String = "modified",
)
