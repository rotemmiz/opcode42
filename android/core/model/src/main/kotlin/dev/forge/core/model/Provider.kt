package dev.forge.core.model

import kotlinx.serialization.Serializable

/** A provider/model pair — the shape POST /session/{id}/message accepts as `model`. */
@Serializable
data class ModelRef(
    val providerID: String,
    val modelID: String,
)

/** One model offered by a provider (GET /provider → all[].models is keyed by id). */
@Serializable
data class ModelInfo(
    val id: String,
    val name: String? = null,
) {
    /** Human label, falling back to the raw id. */
    val label: String get() = name?.takeIf { it.isNotBlank() } ?: id
}

/** A provider surfaced by GET /provider (only the fields the model picker needs). */
@Serializable
data class ProviderInfo(
    val id: String,
    val name: String? = null,
    val models: Map<String, ModelInfo> = emptyMap(),
) {
    val label: String get() = name?.takeIf { it.isNotBlank() } ?: id
}

/** GET /provider → `{ all, default: providerID→modelID, connected: providerID[] }`. */
@Serializable
data class ProvidersResponse(
    val all: List<ProviderInfo> = emptyList(),
    val default: Map<String, String> = emptyMap(),
    val connected: List<String> = emptyList(),
)
