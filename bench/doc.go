// Package bench holds the Opcode42 performance baseline harness (plan 11, W0).
//
// The measurement code lives in files guarded by the `bench` build tag so it is
// excluded from the default `go build ./...` / `go test ./...` and from CI: a
// live benchmark forks real daemons and is intentionally slow. Run it
// explicitly with the tag, e.g.
//
//	go test -tags bench -run TestBaseline -v ./bench/
//
// or, more usefully, through bench/run_baseline.sh which stands up both the real
// opencode daemon and opcoded on the same machine, runs the harness against each,
// and records dated results under bench/results/.
//
// W0 non-negotiable: every number in bench/results/ is an ACTUAL measurement on
// the machine named in the result file. No "Nx faster" claim is committed unless
// both daemons were measured head-to-head in the same run.
package bench
