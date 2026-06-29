package spec

import (
	"fmt"
	"sort"
)

// Drift classifies how a candidate operation set (e.g. Opcode42's self-emitted
// spec) differs from a baseline operation set (the frozen contract), per the
// locked conformance policy (masterplan "Decisions locked" #2):
//
//   - Missing: an operation in the baseline that the candidate does not serve.
//     BREAKING — the daemon dropped a contract operation.
//   - Changed: a matched operation whose declared response status codes differ.
//     BREAKING — the contract's response surface drifted.
//   - Additive: an operation the candidate serves that is not in the baseline.
//     WARN if listed in the known-additions registry; otherwise BREAKING.
//
// Unimplemented operations are NOT a drift signal: Opcode42 registers a 501 stub for
// every reference operation, so they remain present in the emitted spec (the
// locked decision treats unimplemented as 501, not spec-absence).
type Drift struct {
	Missing    []Operation
	Changed    []ChangedOp
	Additive   []Operation // extras not covered by known-additions (BREAKING)
	KnownAdded []Operation // extras allowed by the known-additions registry (WARN)
}

// ChangedOp records a matched operation whose response status codes diverged.
type ChangedOp struct {
	Op       Operation
	Baseline []string
	Emitted  []string
}

// Breaking reports whether the drift contains any FAIL-class finding.
func (d Drift) Breaking() bool {
	return len(d.Missing) > 0 || len(d.Changed) > 0 || len(d.Additive) > 0
}

// CompareOps diffs an emitted operation set against a baseline (reference) set.
// known is the set of additive operations permitted as Opcode42 known-additions;
// such extras land in KnownAdded (WARN) rather than Additive (FAIL).
//
// Each map value is the operation's declared response status codes; pass nil/empty
// to compare presence only.
func CompareOps(baseline, emitted map[Operation][]string, known map[Operation]bool) Drift {
	var d Drift
	for op, baseCodes := range baseline {
		emitCodes, ok := emitted[op]
		if !ok {
			d.Missing = append(d.Missing, op)
			continue
		}
		if len(baseCodes) > 0 && len(emitCodes) > 0 && !sameStrings(baseCodes, emitCodes) {
			d.Changed = append(d.Changed, ChangedOp{Op: op, Baseline: baseCodes, Emitted: emitCodes})
		}
	}
	for op := range emitted {
		if _, ok := baseline[op]; ok {
			continue
		}
		if known[op] {
			d.KnownAdded = append(d.KnownAdded, op)
		} else {
			d.Additive = append(d.Additive, op)
		}
	}
	sortOps(d.Missing)
	sortOps(d.Additive)
	sortOps(d.KnownAdded)
	sort.Slice(d.Changed, func(i, j int) bool {
		return lessOp(d.Changed[i].Op, d.Changed[j].Op)
	})
	return d
}

// Report renders a human-readable, deterministic summary of the drift. The
// returned lines are stable across runs (operations are sorted), so callers can
// print them in CI or assert on them in tests.
func (d Drift) Report() []string {
	var out []string
	for _, op := range d.Missing {
		out = append(out, fmt.Sprintf("BREAKING: MISSING operation %s %s", op.Method, op.Path))
	}
	for _, c := range d.Changed {
		out = append(out, fmt.Sprintf("BREAKING: STATUS CODES changed for %s %s: %v != %v",
			c.Op.Method, c.Op.Path, c.Baseline, c.Emitted))
	}
	for _, op := range d.Additive {
		out = append(out, fmt.Sprintf("BREAKING: EXTRA operation not in reference or known-additions: %s %s", op.Method, op.Path))
	}
	for _, op := range d.KnownAdded {
		out = append(out, fmt.Sprintf("WARN: known-addition %s %s", op.Method, op.Path))
	}
	return out
}

func sortOps(ops []Operation) {
	sort.Slice(ops, func(i, j int) bool { return lessOp(ops[i], ops[j]) })
}

func lessOp(a, b Operation) bool {
	if a.Path != b.Path {
		return a.Path < b.Path
	}
	return a.Method < b.Method
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
