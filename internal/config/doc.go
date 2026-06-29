// Package config loads and merges opencode-format JSONC configuration so Opcode42
// is a drop-in for opencode's config files.
//
// Load order and merge rules mirror opencode (plan 01 §4):
// global config.json → opencode.json → opencode.jsonc, then OPENCODE_CONFIG,
// project files walked up to the worktree, .opencode dirs, OPENCODE_CONFIG_DIR,
// and finally OPENCODE_CONFIG_CONTENT (highest priority). The instructions
// array is concatenated+deduped across layers; all other fields deep-merge
// last-wins (config/config.ts:55-61,443-476,596-674).
//
// Implemented in plan 01 (M1).
package config
