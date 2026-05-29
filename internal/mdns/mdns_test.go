package mdns

import "testing"

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
