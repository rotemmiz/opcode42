package dev.opcode42.core.sdk

// Opcode42Client and HttpTransport are both @Singleton with @Inject constructors, so Hilt wires
// them with no @Provides needed. Their only external dependencies — OkHttpClient (core/network)
// and BaseUrlProvider (bound by :feature:connections ServerConnectionManager) — are provided
// elsewhere. This module is intentionally empty.
