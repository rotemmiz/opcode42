package config

import (
	"encoding/json"
	"testing"
)

// builtin is the set of built-in ids the foundation slice ships.
var builtin = map[string]bool{"gopls": true, "typescript": true, "pyright": true}

func loadLSP(t *testing.T, raw string) (LSPConfig, error) {
	t.Helper()
	var cfg map[string]any
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatalf("unmarshal fixture: %v", err)
		}
	}
	return ParseLSP(cfg, builtin)
}

func TestParseLSP_Absent(t *testing.T) {
	c, err := loadLSP(t, `{}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c.Enabled {
		t.Fatalf("absent lsp should be disabled, got Enabled=true")
	}
	if c.Servers != nil {
		t.Fatalf("absent lsp should have nil Servers")
	}
}

func TestParseLSP_BoolTrue(t *testing.T) {
	c, err := loadLSP(t, `{"lsp": true}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !c.Enabled {
		t.Fatalf("lsp:true should enable")
	}
	if c.Servers != nil {
		t.Fatalf("bool form should not populate Servers")
	}
}

func TestParseLSP_BoolFalse(t *testing.T) {
	c, err := loadLSP(t, `{"lsp": false}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c.Enabled {
		t.Fatalf("lsp:false should disable")
	}
}

func TestParseLSP_MapEnablesBuiltins(t *testing.T) {
	// An override map enables built-ins (opencode treats the presence of the map
	// as "LSP on, with overrides"). An empty object entry is a valid (no-op) entry.
	c, err := loadLSP(t, `{"lsp": {"gopls": {}}}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !c.Enabled {
		t.Fatalf("map form should enable LSP")
	}
	if _, ok := c.Servers["gopls"]; !ok {
		t.Fatalf("gopls entry should be present")
	}
}

func TestParseLSP_InvalidEntryShape(t *testing.T) {
	// Entries are object|{disabled:true} in opencode; a bare bool is not a valid
	// entry and must error cleanly rather than panic.
	if _, err := loadLSP(t, `{"lsp": {"gopls": true}}`); err == nil {
		t.Fatalf("bool entry shape should error")
	}
}

func TestParseLSP_DisabledBuiltin(t *testing.T) {
	c, err := loadLSP(t, `{"lsp": {"gopls": {"disabled": true}}}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !c.Enabled {
		t.Fatalf("map form should enable LSP")
	}
	e, ok := c.Servers["gopls"]
	if !ok {
		t.Fatalf("gopls entry missing")
	}
	if !e.IsDisabled() {
		t.Fatalf("gopls should be disabled")
	}
}

func TestParseLSP_BuiltinOverrideNoExtensionsOK(t *testing.T) {
	// Overriding a built-in's command without extensions is allowed (it's a known id).
	c, err := loadLSP(t, `{"lsp": {"gopls": {"command": ["gopls", "-rpc.trace"]}}}`)
	if err != nil {
		t.Fatalf("built-in override should not require extensions: %v", err)
	}
	e := c.Servers["gopls"]
	if len(e.Command) != 2 || e.Command[0] != "gopls" {
		t.Fatalf("command not decoded: %+v", e.Command)
	}
	if e.IsDisabled() {
		t.Fatalf("override without disabled should stay enabled")
	}
}

func TestParseLSP_CustomServerRequiresExtensions(t *testing.T) {
	_, err := loadLSP(t, `{"lsp": {"mylang": {"command": ["mylang-ls"]}}}`)
	if err == nil {
		t.Fatalf("custom server without extensions should error")
	}
}

func TestParseLSP_CustomServerWithExtensionsOK(t *testing.T) {
	c, err := loadLSP(t, `{"lsp": {"mylang": {"command": ["mylang-ls"], "extensions": [".ml"]}}}`)
	if err != nil {
		t.Fatalf("custom server with extensions should be valid: %v", err)
	}
	e := c.Servers["mylang"]
	if len(e.Extensions) != 1 || e.Extensions[0] != ".ml" {
		t.Fatalf("extensions not decoded: %+v", e.Extensions)
	}
}

func TestParseLSP_DisabledCustomServerNoExtensionsOK(t *testing.T) {
	// A disabled custom entry is exempt from the extensions requirement.
	_, err := loadLSP(t, `{"lsp": {"mylang": {"disabled": true}}}`)
	if err != nil {
		t.Fatalf("disabled custom server should not require extensions: %v", err)
	}
}

func TestLSPEntry_DisabledTracking(t *testing.T) {
	var e LSPEntry
	if err := json.Unmarshal([]byte(`{"command":["x"]}`), &e); err != nil {
		t.Fatal(err)
	}
	if e.IsDisabled() {
		t.Fatalf("absent disabled should report not-disabled")
	}
	var e2 LSPEntry
	if err := json.Unmarshal([]byte(`{"disabled":false}`), &e2); err != nil {
		t.Fatal(err)
	}
	if e2.IsDisabled() {
		t.Fatalf("disabled:false should report not-disabled")
	}
	var e3 LSPEntry
	if err := json.Unmarshal([]byte(`{"disabled":true}`), &e3); err != nil {
		t.Fatal(err)
	}
	if !e3.IsDisabled() {
		t.Fatalf("disabled:true should report disabled")
	}
}
