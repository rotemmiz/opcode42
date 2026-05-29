// Package message is the agent engine's message/part model and storage.
//
// It mirrors opencode's MessageV2 (packages/opencode/src/session/message-v2.ts):
// the User/Assistant message union, the Part union (text, reasoning, file, tool,
// step-start/finish, patch, compaction, subtask), CRUD over plan-01's
// message/part tables (data JSON columns), and the two pure functions the agent
// loop turns on — ToModelMessages (persisted parts -> provider-neutral
// llm.ModelMessage) and FilterCompacted/LatestOf (compaction-aware ordering).
package message
