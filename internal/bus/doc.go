// Package bus is the event fan-out: a per-instance publish/subscribe bus and a
// process-level global bus.
//
// Event payload shape is {id,type,properties} with an auto-assigned id
// (bus/index.ts:24-27,103). The global bus carries a
// {directory?,project?,workspace?,payload} envelope; the global SSE handler
// sends the full envelope, while the instance SSE handler sends the bare
// payload — these two shapes differ and the conformance harness locks both
// (bus/global.ts:4-9; handlers/event.ts vs handlers/global.ts).
//
// Implemented in plan 01 (M4).
package bus
