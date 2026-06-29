package push

import (
	"path/filepath"
	"testing"

	"github.com/rotemmiz/opcode42/internal/storage"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "opcode42.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStore(db.DB)
}

func TestStoreRegisterListUnregister(t *testing.T) {
	s := testStore(t)

	if err := s.Register(Device{DeviceID: "dev1", FCMToken: "tok1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	devices, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("want 1 device, got %d", len(devices))
	}
	d := devices[0]
	if d.DeviceID != "dev1" || d.FCMToken != "tok1" {
		t.Fatalf("unexpected device: %+v", d)
	}
	if d.Platform != "android" {
		t.Errorf("platform default = %q, want android", d.Platform)
	}
	if len(d.SessionFilter) != 1 || d.SessionFilter[0] != "all" {
		t.Errorf("session_filter default = %v, want [all]", d.SessionFilter)
	}

	if err := s.Unregister("dev1"); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	devices, _ = s.List()
	if len(devices) != 0 {
		t.Fatalf("want 0 devices after unregister, got %d", len(devices))
	}
}

func TestStoreRegisterUpsertsToken(t *testing.T) {
	s := testStore(t)
	if err := s.Register(Device{DeviceID: "dev1", FCMToken: "old"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Re-register the same device with a rotated token.
	if err := s.Register(Device{DeviceID: "dev1", FCMToken: "new", SessionFilter: []string{"ses_a"}}); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	devices, _ := s.List()
	if len(devices) != 1 {
		t.Fatalf("want 1 device after upsert, got %d", len(devices))
	}
	if devices[0].FCMToken != "new" {
		t.Errorf("token = %q, want new (upsert)", devices[0].FCMToken)
	}
	if len(devices[0].SessionFilter) != 1 || devices[0].SessionFilter[0] != "ses_a" {
		t.Errorf("session_filter = %v, want [ses_a]", devices[0].SessionFilter)
	}
}

func TestStoreUnregisterNotFound(t *testing.T) {
	s := testStore(t)
	if err := s.Unregister("nope"); err != ErrNotFound {
		t.Fatalf("unregister missing = %v, want ErrNotFound", err)
	}
}

func TestStoreTargetsRespectsFilter(t *testing.T) {
	s := testStore(t)
	mustReg := func(id, tok string, filter []string) {
		if err := s.Register(Device{DeviceID: id, FCMToken: tok, SessionFilter: filter}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	mustReg("all-dev", "t1", []string{"all"})
	mustReg("a-dev", "t2", []string{"ses_a"})
	mustReg("b-dev", "t3", []string{"ses_b"})

	got, err := s.targets("ses_a")
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	ids := map[string]bool{}
	for _, d := range got {
		ids[d.DeviceID] = true
	}
	if !ids["all-dev"] || !ids["a-dev"] || ids["b-dev"] {
		t.Fatalf("targets(ses_a) = %v; want all-dev + a-dev only", ids)
	}
}

func TestRemoveByToken(t *testing.T) {
	s := testStore(t)
	_ = s.Register(Device{DeviceID: "dev1", FCMToken: "dead"})
	s.removeByToken("dead")
	devices, _ := s.List()
	if len(devices) != 0 {
		t.Fatalf("want 0 devices after removeByToken, got %d", len(devices))
	}
}
