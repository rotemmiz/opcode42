package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// errLSPNoServer is returned when no language server handles the file's type.
// The text intentionally matches opencode's user-facing message (tool/lsp.ts:78)
// so the agent sees an identical error.
var errLSPNoServer = errors.New("No LSP server available for this file type.") //nolint:revive,staticcheck // opencode-compat error text

// LSPService is the subset of the per-instance LSP service the lsp tool needs.
// It is an interface (like Asker/Subagent/Skiller) so the tool is unit-testable
// and provider-neutral; *lsp.Service satisfies it. Positions are 0-based — the
// tool converts the agent's 1-based line/character before calling these
// (matching opencode tool/lsp.ts:64). Results are raw LSP JSON, surfaced
// verbatim in the tool output (opencode JSON.stringifies the raw responses).
type LSPService interface {
	HasClients(file string) bool
	TouchFile(ctx context.Context, file string, mode string)
	Hover(file string, line, character int) []json.RawMessage
	Definition(file string, line, character int) []json.RawMessage
	References(file string, line, character int) []json.RawMessage
	Implementation(file string, line, character int) []json.RawMessage
	DocumentSymbol(uri string) []json.RawMessage
	WorkspaceSymbol(query string) []json.RawMessage
	PrepareCallHierarchy(file string, line, character int) []json.RawMessage
	IncomingCalls(file string, line, character int) []json.RawMessage
	OutgoingCalls(file string, line, character int) []json.RawMessage
}

// lspOperations is the fixed string enum the agent passes as `operation`. It
// MUST match opencode tool/lsp.ts:11-22 exactly — getting a name wrong silently
// breaks the model's tool calls (plan 03 review pass, op-name ambiguity).
var lspOperations = []string{
	"goToDefinition",
	"findReferences",
	"hover",
	"documentSymbol",
	"workspaceSymbol",
	"goToImplementation",
	"prepareCallHierarchy",
	"incomingCalls",
	"outgoingCalls",
}

// LSP is the built-in code-intelligence tool. It dispatches one of nine LSP
// operations against the matching language server for a file. Ports opencode's
// tool/lsp.ts. The LSP service is injected at execution time via
// tool.Context.LSP (nil ⇒ the tool reports it is unavailable).
type LSP struct{}

// Info describes the lsp tool. The description mirrors opencode's lsp.txt.
func (LSP) Info() Info {
	return Info{
		ID: "lsp",
		Description: "Interact with Language Server Protocol (LSP) servers to get code intelligence features.\n\n" +
			"Supported operations:\n" +
			"- goToDefinition: Find where a symbol is defined\n" +
			"- findReferences: Find all references to a symbol\n" +
			"- hover: Get hover information (documentation, type info) for a symbol\n" +
			"- documentSymbol: Get all symbols (functions, classes, variables) in a document\n" +
			"- workspaceSymbol: List project-wide symbols matching a query string\n" +
			"- goToImplementation: Find implementations of an interface or abstract method\n" +
			"- prepareCallHierarchy: Get call hierarchy item at a position (functions/methods)\n" +
			"- incomingCalls: Find all functions/methods that call the function at a position\n" +
			"- outgoingCalls: Find all functions/methods called by the function at a position\n\n" +
			"All operations require:\n" +
			"- filePath: The file to operate on\n" +
			"- line: The line number (1-based, as shown in editors)\n" +
			"- character: The character offset (1-based, as shown in editors)\n\n" +
			"workspaceSymbol also accepts:\n" +
			"- query: A query string to filter symbols by. Empty string requests all symbols.\n\n" +
			"Note: LSP servers must be configured for the file type. If no server is available, an error will be returned.",
		Parameters: obj(map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        lspOperations,
				"description": "The LSP operation to perform",
			},
			"filePath": strProp("The absolute or relative path to the file"),
			"line": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "The line number (1-based, as shown in editors)",
			},
			"character": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "The character offset (1-based, as shown in editors)",
			},
			"query": strProp("Search query for workspaceSymbol. Empty string requests all symbols."),
		}, "operation", "filePath", "line", "character"),
	}
}

type lspParams struct {
	Operation string `json:"operation"`
	FilePath  string `json:"filePath"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
	Query     string `json:"query"`
}

// Run dispatches the requested LSP operation. It resolves the file path against
// the working directory, checks the file exists, runs the hasClients pre-check
// and touchFile (lazy spawn + wait), converts the 1-based line/character to
// 0-based, then issues the operation and JSON-encodes the raw results. Ports
// tool/lsp.ts:46-109.
func (LSP) Run(ctx context.Context, input map[string]any, tctx Context) (Result, error) {
	var p lspParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	if !validOperation(p.Operation) {
		return Result{}, fmt.Errorf("lsp: unknown operation %q", p.Operation)
	}
	if tctx.LSP == nil {
		return Result{}, fmt.Errorf("lsp: no LSP service available")
	}

	// Resolve the path against the working directory (tool/lsp.ts:48).
	file := p.FilePath
	if !filepath.IsAbs(file) {
		file = filepath.Join(tctx.Directory, file)
	}
	file = filepath.Clean(file)

	if _, err := os.Stat(file); err != nil {
		// Message intentionally matches opencode's user-facing string (tool/lsp.ts:75).
		return Result{}, fmt.Errorf("File not found: %s", file) //nolint:revive,staticcheck // opencode-compat error text
	}

	// Fast pre-check: no server handles this file type (tool/lsp.ts:77-78).
	if !tctx.LSP.HasClients(file) {
		// Message intentionally matches opencode's user-facing string (tool/lsp.ts:78).
		return Result{}, errLSPNoServer //nolint:revive,staticcheck // opencode-compat error text
	}

	// Lazy spawn + wait for document diagnostics (tool/lsp.ts:80).
	tctx.LSP.TouchFile(ctx, file, "document")

	// 1-based (editor) → 0-based (LSP wire). Ports tool/lsp.ts:64.
	line := p.Line - 1
	character := p.Character - 1

	results := dispatchLSP(tctx.LSP, p, file, line, character)

	return Result{
		Title:    lspTitle(p, file, tctx.Directory),
		Output:   lspOutput(p.Operation, results),
		Metadata: map[string]any{"result": results},
	}, nil
}

// dispatchLSP routes the operation to the LSP service. Ports the switch at
// tool/lsp.ts:83-103. workspaceSymbol uses query (empty string = all symbols);
// documentSymbol takes the file URI; the rest take a 0-based position.
func dispatchLSP(svc LSPService, p lspParams, file string, line, character int) []json.RawMessage {
	switch p.Operation {
	case "goToDefinition":
		return svc.Definition(file, line, character)
	case "findReferences":
		return svc.References(file, line, character)
	case "hover":
		return svc.Hover(file, line, character)
	case "documentSymbol":
		return svc.DocumentSymbol(fileURI(file))
	case "workspaceSymbol":
		return svc.WorkspaceSymbol(p.Query)
	case "goToImplementation":
		return svc.Implementation(file, line, character)
	case "prepareCallHierarchy":
		return svc.PrepareCallHierarchy(file, line, character)
	case "incomingCalls":
		return svc.IncomingCalls(file, line, character)
	case "outgoingCalls":
		return svc.OutgoingCalls(file, line, character)
	}
	return nil
}

// lspOutput renders the model-facing text: the JSON-encoded results, or a "no
// results" message when empty. Ports tool/lsp.ts:108.
func lspOutput(operation string, results []json.RawMessage) string {
	if len(results) == 0 {
		return fmt.Sprintf("No results found for %s", operation)
	}
	b, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Sprintf("No results found for %s", operation)
	}
	return string(b)
}

// lspTitle builds the human label. Ports tool/lsp.ts:65-72: workspaceSymbol has
// no detail; documentSymbol shows the relative path; positional ops show
// path:line:character (the original 1-based values).
func lspTitle(p lspParams, file, directory string) string {
	rel := file
	if r, err := filepath.Rel(directory, file); err == nil {
		rel = r
	}
	switch p.Operation {
	case "workspaceSymbol":
		return p.Operation
	case "documentSymbol":
		return fmt.Sprintf("%s %s", p.Operation, rel)
	default:
		return fmt.Sprintf("%s %s:%d:%d", p.Operation, rel, p.Line, p.Character)
	}
}

// validOperation reports whether op is one of the nine supported operations.
func validOperation(op string) bool {
	for _, o := range lspOperations {
		if o == op {
			return true
		}
	}
	return false
}

// fileURI builds a file:// URI for an absolute path, matching the form gopls and
// other servers expect (pathToFileURL in opencode). filepath-clean already done.
func fileURI(abs string) string {
	return "file://" + abs
}
