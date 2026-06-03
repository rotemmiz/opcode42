package conformance

import (
	"flag"
	"testing"
)

var (
	targetFlag = flag.String("target", "", "daemon base URL to run scenarios against; empty skips the suite")
	outFlag    = flag.String("out", "", "write the result JSON to this path")
	userFlag   = flag.String("user", "", "Basic-auth username (for auth scenarios)")
	passFlag   = flag.String("pass", "", "Basic-auth password (for auth scenarios)")
)

// TestSuite runs the scenario set against -target and optionally writes the
// result to -out. With no -target it skips, so `go test ./...` stays hermetic.
//
//	go test ./conformance/ -run TestSuite -target=http://127.0.0.1:4096 -out=results/opencode.json
func TestSuite(t *testing.T) {
	if *targetFlag == "" {
		t.Skip("no -target; skipping conformance suite (set -target=http://host:port)")
	}
	f, err := Run(*targetFlag, *userFlag, *passFlag)
	if err != nil {
		t.Fatalf("run suite: %v", err)
	}
	if *outFlag != "" {
		if err := f.Save(*outFlag); err != nil {
			t.Fatalf("save result: %v", err)
		}
		t.Logf("wrote %d scenarios to %s", len(f.Scenarios), *outFlag)
	}
}

// TestLiveSuite runs the live (LLM-driven) scenarios against -target and writes
// the result to -out. Like TestSuite it skips without -target, so `go test ./...`
// never touches the network. The live dual-run (scripts/run-conformance.sh live)
// is the only caller that sets -target here, and it injects the pinned-model
// provider key into BOTH spawned daemons. Without the key the scenarios would
// error at the provider, which is why CI without the key must not invoke this —
// the skip-gate is the empty -target.
//
//	go test ./conformance/ -run TestLiveSuite -target=http://127.0.0.1:4096 -out=results/live-opencode.json
func TestLiveSuite(t *testing.T) {
	if *targetFlag == "" {
		t.Skip("no -target; skipping live conformance suite (set -target=http://host:port)")
	}
	f, err := RunLive(*targetFlag, *userFlag, *passFlag)
	if err != nil {
		t.Fatalf("run live suite: %v", err)
	}
	if *outFlag != "" {
		if err := f.Save(*outFlag); err != nil {
			t.Fatalf("save result: %v", err)
		}
		t.Logf("wrote %d live scenarios to %s", len(f.Scenarios), *outFlag)
	}
}
