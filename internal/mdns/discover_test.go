package mdns

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/grandcat/zeroconf"
)

// registerLoopback publishes an mDNS service on the loopback interface with an
// explicit 127.0.0.1 A record. zeroconf.Register (without proxy) skips
// loopback IPs (addrsForInterface in server.go:654), so a loopback test must
// use RegisterProxy with an explicit IP. The returned server must be Shutdown
// at test end.
func registerLoopback(t *testing.T, instance, serviceType string, port int, txt []string) *zeroconf.Server {
	t.Helper()
	srv, err := zeroconf.RegisterProxy(
		instance, serviceType, "local.", port, "opencode.local",
		[]string{"127.0.0.1"}, txt, nil,
	)
	if err != nil {
		t.Fatalf("RegisterProxy(%s, %s): %v", instance, serviceType, err)
	}
	return srv
}

// TestBrowse_LoopbackRoundTrip publishes an opcode42-<port> service on
// _http._tcp and asserts Browse surfaces it within 5s. This is the core
// client-side mirror of the daemon's Publish (mdns.go:66).
func TestBrowse_LoopbackRoundTrip(t *testing.T) {
	port := freePort(t)
	instance := "opcode42-" + strconv.Itoa(port)
	srv := registerLoopback(t, instance, "_http._tcp.", port, []string{"path=/"})
	defer srv.Shutdown()

	// Give the publisher a moment to probe + announce before browsing.
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := Browse(ctx, []string{"_http._tcp."}, []string{"opcode42-"})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}

	var found *DiscoveredService
	for svc := range out {
		if svc.Name == instance && svc.Port == port {
			cp := svc
			found = &cp
			break
		}
	}
	cancel()

	if found == nil {
		t.Fatalf("did not discover %s on _http._tcp within timeout", instance)
	}
	if found.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1", found.Host)
	}
	if found.Port != port {
		t.Errorf("Port = %d, want %d", found.Port, port)
	}
	if len(found.TXT) != 1 || found.TXT[0] != "path=/" {
		t.Errorf("TXT = %v, want [path=/]", found.TXT)
	}
}

// TestBrowse_FilterByPrefix publishes two services — one matching the prefix
// (opcode42-<port>) and one not (other-service) — and asserts Browse surfaces
// only the matching one. The matching service doubles as a control: if it
// arrives, mDNS is working on loopback, so the absence of other-service is
// attributable to the prefix filter, not a transport failure.
func TestBrowse_FilterByPrefix(t *testing.T) {
	matchPort := freePort(t)
	matchInstance := "opcode42-" + strconv.Itoa(matchPort)
	matchSrv := registerLoopback(t, matchInstance, "_http._tcp.", matchPort, []string{"path=/"})
	defer matchSrv.Shutdown()

	otherPort := freePort(t)
	otherSrv := registerLoopback(t, "other-service", "_http._tcp.", otherPort, []string{"path=/"})
	defer otherSrv.Shutdown()

	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := Browse(ctx, []string{"_http._tcp."}, []string{"opcode42-"})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}

	var sawMatch, sawOther bool
	var otherSvc DiscoveredService
	for svc := range out {
		switch svc.Name {
		case matchInstance:
			sawMatch = true
		case "other-service":
			sawOther = true
			otherSvc = svc
		}
	}

	if !sawMatch {
		t.Fatalf("control service %s did not arrive — mDNS loopback transport not working in this env", matchInstance)
	}
	if sawOther {
		t.Errorf("filter should have dropped other-service, but it surfaced: host=%s port=%d", otherSvc.Host, otherSvc.Port)
	}
}

// TestBrowse_DedupeAcrossServiceTypes publishes the SAME (host, port) on both
// _http._tcp and _opencode._tcp (the daemon's dual-publish shape) and asserts
// Browse surfaces it exactly once across the two service types.
func TestBrowse_DedupeAcrossServiceTypes(t *testing.T) {
	port := freePort(t)
	instance := "opcode42-" + strconv.Itoa(port)
	httpSrv := registerLoopback(t, instance, "_http._tcp.", port, []string{"path=/"})
	defer httpSrv.Shutdown()
	ocSrv := registerLoopback(t, instance, "_opencode._tcp.", port, []string{"path=/", "auth=open", "version=1"})
	defer ocSrv.Shutdown()

	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := Browse(ctx, []string{"_http._tcp.", "_opencode._tcp."}, []string{"opcode42-"})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}

	var count int
	var first *DiscoveredService
	for svc := range out {
		if svc.Name == instance && svc.Port == port {
			count++
			if count == 1 {
				cp := svc
				first = &cp
			}
		}
	}
	cancel()

	if count == 0 {
		t.Fatalf("did not discover %s within timeout", instance)
	}
	if count > 1 {
		t.Errorf("discovered %s %d times, want 1 (dedupe by host:port failed)", instance, count)
	}
	if first != nil && first.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1", first.Host)
	}
}

// TestBrowse_CancelContext asserts that canceling the caller's context stops
// the browser and closes the output channel promptly (the connect overlay in
// D2 closes the overlay → cancels ctx → browser must stop).
func TestBrowse_CancelContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out, err := Browse(ctx, nil, nil)
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}

	cancel()

	// A stray service on the LAN could surface before close; that's fine.
	// The contract is "channel closes after cancel", not "no service ever
	// surfaces". Drain whatever arrives until close, with a hard timeout.
	closed := make(chan struct{})
	go func() {
		for range out { //nolint:revive // intentional drain
		}
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("output channel did not close within 2s of ctx cancel")
	}
}

// TestBrowse_Defaults asserts that empty serviceTypes/namePrefixes fall back to
// the documented defaults (both service types, both name prefixes) without
// panic and with a promptly-closing channel on cancel.
func TestBrowse_Defaults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out, err := Browse(ctx, nil, nil)
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	cancel()

	closed := make(chan struct{})
	go func() {
		for range out { //nolint:revive // intentional drain
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("output channel did not close within 2s of ctx cancel")
	}
}
