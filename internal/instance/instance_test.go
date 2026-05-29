package instance

import (
	"testing"
	"time"

	"github.com/rotemmiz/forge/internal/bus"
)

func TestGetIsCachedAndPerDirectory(t *testing.T) {
	m := NewManager(bus.NewGlobal())
	a1 := m.Get("/dir/a")
	a2 := m.Get("/dir/a")
	b := m.Get("/dir/b")
	if a1 != a2 {
		t.Error("Get must return the cached instance for the same directory")
	}
	if a1 == b {
		t.Error("different directories must get different instances")
	}
	if a1.Bus == nil || a1.Pty == nil {
		t.Error("instance Context must have a Bus and Pty")
	}
}

func TestDisposeAllEmitsDisposed(t *testing.T) {
	m := NewManager(bus.NewGlobal())
	c := m.Get("/dir")
	events, unsub := c.Bus.Subscribe()
	defer unsub()

	m.DisposeAll()

	select {
	case e := <-events:
		if e.Type != bus.EventInstanceDisposed {
			t.Errorf("got %q, want %q", e.Type, bus.EventInstanceDisposed)
		}
	case <-time.After(time.Second):
		t.Fatal("DisposeAll did not emit server.instance.disposed")
	}

	// The cache is cleared: a subsequent Get builds a fresh instance.
	if m.Get("/dir") == c {
		t.Error("DisposeAll should clear the cache")
	}
}
