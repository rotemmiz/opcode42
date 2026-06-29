package mdns

import (
	"net"
	"testing"
)

// freePort returns an OS-assigned free TCP port (zeroconf rejects port 0).
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// TestPublishDualRecords asserts Publish registers both the opencode-compatible
// _http._tcp record and Opcode42's _opencode._tcp record (plan 13 §Discovery), and
// that Shutdown withdraws both and is idempotent / nil-safe.
func TestPublishDualRecords(t *testing.T) {
	svc, err := Publish(freePort(t), "", true)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got := len(svc.servers); got != 2 {
		t.Fatalf("published %d records, want 2 (_http._tcp + _opencode._tcp)", got)
	}
	svc.Shutdown()
	if svc.servers != nil {
		t.Errorf("Shutdown should clear servers, got %v", svc.servers)
	}
	svc.Shutdown()             // idempotent
	(*Service)(nil).Shutdown() // nil-safe
}

func TestIsLoopback(t *testing.T) {
	for _, h := range []string{"127.0.0.1", "localhost", "::1", "", "LOCALHOST", " 127.0.0.1 "} {
		if !IsLoopback(h) {
			t.Errorf("IsLoopback(%q) = false, want true", h)
		}
	}
	for _, h := range []string{"0.0.0.0", "192.168.1.5", "example.local", "10.0.0.1"} {
		if IsLoopback(h) {
			t.Errorf("IsLoopback(%q) = true, want false", h)
		}
	}
}

func TestShouldPublish(t *testing.T) {
	if ShouldPublish(false, "0.0.0.0") {
		t.Error("disabled mDNS must not publish")
	}
	if ShouldPublish(true, "127.0.0.1") {
		t.Error("must not publish on a loopback host")
	}
	if !ShouldPublish(true, "0.0.0.0") {
		t.Error("enabled mDNS on a non-loopback host should publish")
	}
}
