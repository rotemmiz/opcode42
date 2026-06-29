package spec

import "testing"

func ops(pairs ...[2]string) map[Operation][]string {
	m := map[Operation][]string{}
	for _, p := range pairs {
		m[Operation{Method: p[0], Path: p[1]}] = []string{"200"}
	}
	return m
}

// TestCompareOpsClean: identical sets produce no drift.
func TestCompareOpsClean(t *testing.T) {
	base := ops([2]string{"GET", "/session"}, [2]string{"POST", "/session"})
	emit := ops([2]string{"GET", "/session"}, [2]string{"POST", "/session"})
	d := CompareOps(base, emit, nil)
	if d.Breaking() {
		t.Fatalf("expected clean, got %v", d.Report())
	}
}

// TestCompareOpsCatchesMissing: an operation dropped from the emitted spec is a
// BREAKING miss. This is the core teeth: the gate must fail when a handler/route
// disappears.
func TestCompareOpsCatchesMissing(t *testing.T) {
	base := ops([2]string{"GET", "/session"}, [2]string{"POST", "/session"})
	emit := ops([2]string{"GET", "/session"}) // POST /session dropped
	d := CompareOps(base, emit, nil)
	if !d.Breaking() {
		t.Fatal("expected BREAKING for missing operation, got clean")
	}
	if len(d.Missing) != 1 || d.Missing[0] != (Operation{Method: "POST", Path: "/session"}) {
		t.Fatalf("expected POST /session missing, got %+v", d.Missing)
	}
}

// TestCompareOpsCatchesChangedStatus: a matched operation whose response codes
// changed is BREAKING.
func TestCompareOpsCatchesChangedStatus(t *testing.T) {
	base := map[Operation][]string{{Method: "GET", Path: "/session"}: {"200"}}
	emit := map[Operation][]string{{Method: "GET", Path: "/session"}: {"200", "404"}}
	d := CompareOps(base, emit, nil)
	if !d.Breaking() {
		t.Fatal("expected BREAKING for changed status codes, got clean")
	}
	if len(d.Changed) != 1 {
		t.Fatalf("expected 1 changed op, got %+v", d.Changed)
	}
}

// TestCompareOpsExtraUnknownIsBreaking: an additive operation NOT in the
// known-additions registry is BREAKING (FAIL).
func TestCompareOpsExtraUnknownIsBreaking(t *testing.T) {
	base := ops([2]string{"GET", "/session"})
	emit := ops([2]string{"GET", "/session"}, [2]string{"GET", "/opcode42/secret"})
	d := CompareOps(base, emit, nil)
	if !d.Breaking() {
		t.Fatal("expected BREAKING for unknown additive op, got clean")
	}
	if len(d.Additive) != 1 || d.Additive[0].Path != "/opcode42/secret" {
		t.Fatalf("expected /opcode42/secret additive, got %+v", d.Additive)
	}
}

// TestCompareOpsExtraKnownIsWarn: an additive operation listed in known-additions
// is a WARN, not a FAIL.
func TestCompareOpsExtraKnownIsWarn(t *testing.T) {
	base := ops([2]string{"GET", "/session"})
	emit := ops([2]string{"GET", "/session"}, [2]string{"GET", "/openapi.json"})
	known := map[Operation]bool{{Method: "GET", Path: "/openapi.json"}: true}
	d := CompareOps(base, emit, known)
	if d.Breaking() {
		t.Fatalf("expected clean (known-addition is WARN), got %v", d.Report())
	}
	if len(d.KnownAdded) != 1 {
		t.Fatalf("expected 1 known-addition, got %+v", d.KnownAdded)
	}
}

func TestParseKnownAdditions(t *testing.T) {
	known, err := ParseKnownAdditions([]byte(`[["get", "/openapi.json"]]`))
	if err != nil {
		t.Fatalf("ParseKnownAdditions: %v", err)
	}
	if !known[Operation{Method: "GET", Path: "/openapi.json"}] {
		t.Fatalf("expected GET /openapi.json in set, got %+v", known)
	}
}

func TestParseKnownAdditionsRejectsMalformed(t *testing.T) {
	if _, err := ParseKnownAdditions([]byte(`[["only-one"]]`)); err == nil {
		t.Fatal("expected error for malformed entry")
	}
}
