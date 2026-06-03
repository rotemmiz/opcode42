package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLSP records the call it received and returns canned results, so the tool's
// dispatch, coordinate conversion, and pre-checks can be asserted in isolation.
type fakeLSP struct {
	hasClients bool
	touched    string
	touchMode  string

	op        string // last query op invoked
	file      string
	uri       string
	line      int // 0-based as received by the service
	character int
	query     string

	result []json.RawMessage
}

func (f *fakeLSP) HasClients(_ string) bool { return f.hasClients }
func (f *fakeLSP) TouchFile(_ context.Context, file, mode string) {
	f.touched, f.touchMode = file, mode
}
func (f *fakeLSP) record(op, file string, line, character int) []json.RawMessage {
	f.op, f.file, f.line, f.character = op, file, line, character
	return f.result
}
func (f *fakeLSP) Hover(file string, line, character int) []json.RawMessage {
	return f.record("hover", file, line, character)
}
func (f *fakeLSP) Definition(file string, line, character int) []json.RawMessage {
	return f.record("definition", file, line, character)
}
func (f *fakeLSP) References(file string, line, character int) []json.RawMessage {
	return f.record("references", file, line, character)
}
func (f *fakeLSP) Implementation(file string, line, character int) []json.RawMessage {
	return f.record("implementation", file, line, character)
}
func (f *fakeLSP) DocumentSymbol(uri string) []json.RawMessage {
	f.op, f.uri = "documentSymbol", uri
	return f.result
}
func (f *fakeLSP) WorkspaceSymbol(query string) []json.RawMessage {
	f.op, f.query = "workspaceSymbol", query
	return f.result
}
func (f *fakeLSP) PrepareCallHierarchy(file string, line, character int) []json.RawMessage {
	return f.record("prepareCallHierarchy", file, line, character)
}
func (f *fakeLSP) IncomingCalls(file string, line, character int) []json.RawMessage {
	return f.record("incomingCalls", file, line, character)
}
func (f *fakeLSP) OutgoingCalls(file string, line, character int) []json.RawMessage {
	return f.record("outgoingCalls", file, line, character)
}

func lspCtx(dir string, svc LSPService) Context {
	c := tctx(dir)
	c.LSP = svc
	return c
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The agent passes 1-based line/character; the LSP wire is 0-based. The tool
// must subtract one before calling the service (opencode tool/lsp.ts:64).
func TestLSP_ConvertsOneBasedToZeroBased(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n")
	svc := &fakeLSP{hasClients: true, result: []json.RawMessage{json.RawMessage(`{"ok":true}`)}}

	_, err := (LSP{}).Run(context.Background(), map[string]any{
		"operation": "goToDefinition", "filePath": "main.go", "line": 10, "character": 5,
	}, lspCtx(dir, svc))
	if err != nil {
		t.Fatal(err)
	}
	if svc.op != "definition" {
		t.Fatalf("op = %q, want definition", svc.op)
	}
	if svc.line != 9 || svc.character != 4 {
		t.Fatalf("position = (%d,%d), want (9,4)", svc.line, svc.character)
	}
}

func TestLSP_RejectsUnknownOperation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n")
	_, err := (LSP{}).Run(context.Background(), map[string]any{
		"operation": "definition", "filePath": "main.go", "line": 1, "character": 1,
	}, lspCtx(dir, &fakeLSP{hasClients: true}))
	if err == nil || !strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("err = %v, want unknown operation", err)
	}
}

func TestLSP_NoClientsErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "x")
	_, err := (LSP{}).Run(context.Background(), map[string]any{
		"operation": "hover", "filePath": "a.txt", "line": 1, "character": 1,
	}, lspCtx(dir, &fakeLSP{hasClients: false}))
	if err == nil || !strings.Contains(err.Error(), "No LSP server") {
		t.Fatalf("err = %v, want No LSP server", err)
	}
}

func TestLSP_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := (LSP{}).Run(context.Background(), map[string]any{
		"operation": "hover", "filePath": "missing.go", "line": 1, "character": 1,
	}, lspCtx(dir, &fakeLSP{hasClients: true}))
	if err == nil || !strings.Contains(err.Error(), "File not found") {
		t.Fatalf("err = %v, want File not found", err)
	}
}

func TestLSP_NilServiceUnavailable(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n")
	_, err := (LSP{}).Run(context.Background(), map[string]any{
		"operation": "hover", "filePath": "main.go", "line": 1, "character": 1,
	}, tctx(dir))
	if err == nil || !strings.Contains(err.Error(), "no LSP service") {
		t.Fatalf("err = %v, want no LSP service", err)
	}
}

func TestLSP_TouchesBeforeQuery(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, dir, "main.go", "package main\n")
	svc := &fakeLSP{hasClients: true}
	if _, err := (LSP{}).Run(context.Background(), map[string]any{
		"operation": "hover", "filePath": "main.go", "line": 1, "character": 1,
	}, lspCtx(dir, svc)); err != nil {
		t.Fatal(err)
	}
	if svc.touched != file {
		t.Fatalf("touched = %q, want %q", svc.touched, file)
	}
	if svc.touchMode != "document" {
		t.Fatalf("touchMode = %q, want document", svc.touchMode)
	}
}

func TestLSP_DocumentSymbolUsesFileURI(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, dir, "main.go", "package main\n")
	svc := &fakeLSP{hasClients: true}
	if _, err := (LSP{}).Run(context.Background(), map[string]any{
		"operation": "documentSymbol", "filePath": "main.go", "line": 1, "character": 1,
	}, lspCtx(dir, svc)); err != nil {
		t.Fatal(err)
	}
	want := "file://" + file
	if svc.uri != want {
		t.Fatalf("uri = %q, want %q", svc.uri, want)
	}
}

func TestLSP_WorkspaceSymbolUsesQuery(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n")
	svc := &fakeLSP{hasClients: true}
	if _, err := (LSP{}).Run(context.Background(), map[string]any{
		"operation": "workspaceSymbol", "filePath": "main.go", "line": 1, "character": 1, "query": "Foo",
	}, lspCtx(dir, svc)); err != nil {
		t.Fatal(err)
	}
	if svc.query != "Foo" {
		t.Fatalf("query = %q, want Foo", svc.query)
	}
}

func TestLSP_NoResultsMessage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n")
	res, err := (LSP{}).Run(context.Background(), map[string]any{
		"operation": "findReferences", "filePath": "main.go", "line": 2, "character": 3,
	}, lspCtx(dir, &fakeLSP{hasClients: true})) // result is nil
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "No results found for findReferences" {
		t.Fatalf("output = %q", res.Output)
	}
}

func TestLSP_TitleFormats(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n")
	svc := &fakeLSP{hasClients: true, result: []json.RawMessage{json.RawMessage(`1`)}}

	cases := []struct{ op, want string }{
		{"hover", "hover main.go:7:3"},
		{"documentSymbol", "documentSymbol main.go"},
		{"workspaceSymbol", "workspaceSymbol"},
	}
	for _, c := range cases {
		res, err := (LSP{}).Run(context.Background(), map[string]any{
			"operation": c.op, "filePath": "main.go", "line": 7, "character": 3,
		}, lspCtx(dir, svc))
		if err != nil {
			t.Fatalf("%s: %v", c.op, err)
		}
		if res.Title != c.want {
			t.Fatalf("%s: title = %q, want %q", c.op, res.Title, c.want)
		}
	}
}

// Every enum value routes to a distinct service method (guards against silent
// op-name drift from opencode tool/lsp.ts:11-22).
func TestLSP_AllOperationsDispatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n")
	want := map[string]string{
		"goToDefinition":       "definition",
		"findReferences":       "references",
		"hover":                "hover",
		"goToImplementation":   "implementation",
		"prepareCallHierarchy": "prepareCallHierarchy",
		"incomingCalls":        "incomingCalls",
		"outgoingCalls":        "outgoingCalls",
	}
	for op, method := range want {
		svc := &fakeLSP{hasClients: true}
		if _, err := (LSP{}).Run(context.Background(), map[string]any{
			"operation": op, "filePath": "main.go", "line": 1, "character": 1,
		}, lspCtx(dir, svc)); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if svc.op != method {
			t.Fatalf("%s dispatched to %q, want %q", op, svc.op, method)
		}
	}
}

// The advertised parameter schema must expose the exact opencode operation enum.
func TestLSP_SchemaEnumMatchesOpencode(t *testing.T) {
	info := LSP{}.Info()
	props := info.Parameters["properties"].(map[string]any)
	enum := props["operation"].(map[string]any)["enum"].([]string)
	want := []string{
		"goToDefinition", "findReferences", "hover", "documentSymbol", "workspaceSymbol",
		"goToImplementation", "prepareCallHierarchy", "incomingCalls", "outgoingCalls",
	}
	if len(enum) != len(want) {
		t.Fatalf("enum = %v, want %v", enum, want)
	}
	for i := range want {
		if enum[i] != want[i] {
			t.Fatalf("enum[%d] = %q, want %q", i, enum[i], want[i])
		}
	}
}
