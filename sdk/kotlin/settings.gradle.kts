// Standalone settings so the Kotlin SDK builds on its own (CI sdk-fresh job).
// The Android app does NOT include this module yet; it ships its own hand-written
// client in core:sdk and can migrate onto sdk/kotlin/gen when ready (plan 07).
rootProject.name = "forge-sdk-kotlin"
