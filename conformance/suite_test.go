package conformance

import (
	"flag"
	"testing"
)

var (
	targetFlag = flag.String("target", "", "daemon base URL to run scenarios against; empty skips the suite")
	outFlag    = flag.String("out", "", "write the result JSON to this path")
)

// TestSuite runs the scenario set against -target and optionally writes the
// result to -out. With no -target it skips, so `go test ./...` stays hermetic.
//
//	go test ./conformance/ -run TestSuite -target=http://127.0.0.1:4096 -out=results/opencode.json
func TestSuite(t *testing.T) {
	if *targetFlag == "" {
		t.Skip("no -target; skipping conformance suite (set -target=http://host:port)")
	}
	f, err := Run(*targetFlag)
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
