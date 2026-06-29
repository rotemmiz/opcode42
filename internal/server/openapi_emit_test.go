package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rotemmiz/opcode42/internal/api/spec"
)

// TestEmittedSpecDiffsCleanAgainstFrozenContract is the offline drift gate (plan
// 06 Phase 2 / M10). It builds a fully-wired daemon, fetches the spec Opcode42
// self-emits at GET /openapi.json (derived from the daemon's actual route table),
// and classifies it against the frozen reference:
//
//   - missing / changed operation  -> FAIL
//   - additive operation           -> FAIL unless in conformance/known-additions.json (WARN)
//
// This has teeth that the old check-spec-drift.sh lacked: that script served the
// reference verbatim at /doc and so compared the contract to itself. Here the
// served spec is route-table-derived, so dropping a reg(...) or adding an
// unspec'd route changes the emitted spec and trips the gate (the spec-package
// drift_test.go proves the classifier catches missing/changed/extra).
func TestEmittedSpecDiffsCleanAgainstFrozenContract(t *testing.T) {
	h := fullyWiredServer(t)

	r := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d; body=%s", rr.Code, rr.Body.String())
	}

	emitted, err := spec.EmittedOperations(rr.Body.Bytes())
	if err != nil {
		t.Fatalf("parse emitted spec: %v", err)
	}
	baseline, err := spec.EmittedOperations(spec.Reference())
	if err != nil {
		t.Fatalf("parse reference: %v", err)
	}

	known, err := spec.ParseKnownAdditions(readKnownAdditions(t))
	if err != nil {
		t.Fatalf("parse known-additions: %v", err)
	}

	drift := spec.CompareOps(baseline, emitted, known)
	for _, line := range drift.Report() {
		t.Log(line)
	}
	if drift.Breaking() {
		t.Fatalf("emitted spec drifts from frozen contract (see log above): "+
			"%d missing, %d changed, %d unsanctioned additions",
			len(drift.Missing), len(drift.Changed), len(drift.Additive))
	}

	// Sanity: the fully-wired daemon must serve at least the whole frozen contract
	// (it registers a 501 stub for every reference op), so emitted ⊇ baseline.
	if len(emitted) < len(baseline) {
		t.Fatalf("emitted ops (%d) < reference ops (%d)", len(emitted), len(baseline))
	}
}

// TestOpenAPIJSONIsSelfEmittedNotVerbatim asserts /openapi.json serves the
// route-table-derived spec, distinct from the verbatim reference at /doc — so the
// emit path is actually exercised (not the old alias behavior).
func TestOpenAPIJSONIsSelfEmittedNotVerbatim(t *testing.T) {
	h := fullyWiredServer(t)

	docRR := httptest.NewRecorder()
	h.ServeHTTP(docRR, httptest.NewRequest(http.MethodGet, "/doc", nil))
	emitRR := httptest.NewRecorder()
	h.ServeHTTP(emitRR, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))

	if docRR.Code != http.StatusOK || emitRR.Code != http.StatusOK {
		t.Fatalf("/doc=%d /openapi.json=%d", docRR.Code, emitRR.Code)
	}
	if docRR.Body.String() != string(spec.Reference()) {
		t.Error("/doc must serve the frozen reference verbatim (opencode parity)")
	}
	// /openapi.json is regenerated/re-marshaled from the route table, so it is not
	// byte-identical to the embedded reference.
	if emitRR.Body.String() == string(spec.Reference()) {
		t.Error("/openapi.json must be self-emitted, not the verbatim reference")
	}
}

func readKnownAdditions(t *testing.T) []byte {
	t.Helper()
	// Walk up to the repo root to find conformance/known-additions.json regardless
	// of the package's working directory.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		p := filepath.Join(dir, "conformance", "known-additions.json")
		if data, err := os.ReadFile(p); err == nil {
			return data
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not locate conformance/known-additions.json")
	return nil
}
