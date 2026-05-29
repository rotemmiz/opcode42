// Package id generates prefixed, lexicographically-monotonic identifiers
// (e.g. ses_, msg_, prt_, prj_, evt_) using ULIDs with monotonic entropy,
// matching the semantics of opencode's Identifier.create so IDs sort in
// creation order for stable pagination.
//
// Implemented in plan 01 (M2/M4).
package id
