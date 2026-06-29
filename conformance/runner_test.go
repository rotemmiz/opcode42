package conformance

import (
	"net/http/httptest"
	"testing"

	"github.com/rotemmiz/opcode42/internal/server"
)

// TestRunnerAgainstOpcodedSkeleton exercises the whole runner against the
// in-process opcoded skeleton (no opencode needed). The skeleton 501s the
// documented endpoints, so this proves the framework runs end-to-end and
// produces a well-formed result file; it is not a conformance assertion.
func TestRunnerAgainstOpcodedSkeleton(t *testing.T) {
	h, err := server.New(server.Options{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	f, err := Run(srv.URL, "", "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(f.Scenarios) != len(Scenarios) {
		t.Fatalf("scenario count: want %d, got %d", len(Scenarios), len(f.Scenarios))
	}
	// session-create-list should record a 'create' POST that the skeleton 501s.
	sc := f.ScenarioByName("session-create-list")
	if sc == nil || len(sc.Steps) == 0 {
		t.Fatal("missing session-create-list steps")
	}
	if sc.Steps[0].Status != 501 {
		t.Errorf("opcoded skeleton should 501 POST /session, got %d", sc.Steps[0].Status)
	}
	if sc.Steps[0].Path != "/session" {
		t.Errorf("path: want /session, got %q", sc.Steps[0].Path)
	}
}
