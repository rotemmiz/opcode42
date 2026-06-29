package lsp

import (
	"context"
	"encoding/json"

	"go.lsp.dev/uri"
)

// queryRequestTimeout bounds a single LSP query request. opencode relies on the
// vscode-jsonrpc default (no explicit per-request timeout on query calls,
// lsp/lsp.ts:381-482); Opcode42 bounds each call so a wedged server cannot hang the
// agent loop. It reuses the diagnostics request budget (3s).
const queryRequestTimeout = diagnosticsRequestWait

// Position is a 0-based LSP position. The lsp tool converts the agent's 1-based
// line/character to 0-based before calling these methods (tool/lsp.go), matching
// opencode tool/lsp.ts:64 (line - 1, character - 1).
type Position struct {
	Line      int
	Character int
}

// textDocumentRequest issues a textDocument/<method> request anchored at a
// file+position and returns the raw JSON result, or nil on any error (mirroring
// opencode's `.catch(() => null)` / `.catch(() => [])` per query method). The
// raw shape is preserved (the tool JSON-stringifies it as-is, like opencode).
func (c *Client) textDocumentRequest(ctx context.Context, method, file string, pos Position, extra map[string]any) json.RawMessage {
	abs := c.resolve(file)
	params := map[string]any{
		"textDocument": map[string]any{"uri": string(uri.File(abs))},
		"position":     map[string]any{"line": pos.Line, "character": pos.Character},
	}
	for k, v := range extra {
		params[k] = v
	}
	return c.call(ctx, method, params)
}

// call issues a raw JSON-RPC request and returns the result as raw JSON, or nil
// on error. The result is captured into a json.RawMessage so the exact LSP wire
// shape flows through unchanged (opencode passes the raw responses to
// JSON.stringify; Opcode42 does the same).
func (c *Client) call(ctx context.Context, method string, params any) json.RawMessage {
	rctx, cancel := context.WithTimeout(ctx, queryRequestTimeout)
	defer cancel()
	var raw json.RawMessage
	if _, err := c.conn.Call(rctx, method, params, &raw); err != nil {
		return nil
	}
	return raw
}

// Hover returns the textDocument/hover result for the position, or nil. Ports
// LSP.hover (lsp.ts:381-390): a single result (the tool wraps it per-client).
func (c *Client) Hover(ctx context.Context, file string, pos Position) json.RawMessage {
	return c.textDocumentRequest(ctx, "textDocument/hover", file, pos, nil)
}

// Definition returns the textDocument/definition result (Location | Location[] |
// LocationLink[]), or nil. Ports LSP.definition (lsp.ts:392-402).
func (c *Client) Definition(ctx context.Context, file string, pos Position) json.RawMessage {
	return c.textDocumentRequest(ctx, "textDocument/definition", file, pos, nil)
}

// References returns textDocument/references with includeDeclaration=true, or
// nil. Ports LSP.references (lsp.ts:404-415).
func (c *Client) References(ctx context.Context, file string, pos Position) json.RawMessage {
	return c.textDocumentRequest(ctx, "textDocument/references", file, pos, map[string]any{
		"context": map[string]any{"includeDeclaration": true},
	})
}

// Implementation returns textDocument/implementation, or nil. Ports
// LSP.implementation (lsp.ts:417-427).
func (c *Client) Implementation(ctx context.Context, file string, pos Position) json.RawMessage {
	return c.textDocumentRequest(ctx, "textDocument/implementation", file, pos, nil)
}

// DocumentSymbol returns textDocument/documentSymbol for uriStr (a file:// URI),
// or nil. Ports LSP.documentSymbol (lsp.ts:429-435).
func (c *Client) DocumentSymbol(ctx context.Context, uriStr string) json.RawMessage {
	return c.call(ctx, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uriStr},
	})
}

// WorkspaceSymbol returns workspace/symbol filtered to the curated kinds and
// capped at 10 per client. Ports LSP.workspaceSymbol (lsp.ts:437-445).
func (c *Client) WorkspaceSymbol(ctx context.Context, query string) []json.RawMessage {
	raw := c.call(ctx, "workspace/symbol", map[string]any{"query": query})
	if raw == nil {
		return nil
	}
	var symbols []json.RawMessage
	if err := json.Unmarshal(raw, &symbols); err != nil {
		return nil
	}
	out := make([]json.RawMessage, 0, len(symbols))
	for _, s := range symbols {
		if !symbolKindIncluded(s) {
			continue
		}
		out = append(out, s)
		if len(out) >= workspaceSymbolLimit {
			break
		}
	}
	return out
}

// PrepareCallHierarchy returns textDocument/prepareCallHierarchy, or nil. Ports
// LSP.prepareCallHierarchy (lsp.ts:447-457).
func (c *Client) PrepareCallHierarchy(ctx context.Context, file string, pos Position) json.RawMessage {
	return c.textDocumentRequest(ctx, "textDocument/prepareCallHierarchy", file, pos, nil)
}

// IncomingCalls returns callHierarchy/incomingCalls for the first call-hierarchy
// item at the position, or nil if there is no item. Ports the
// callHierarchyRequest helper (lsp.ts:459-474) for the incoming direction.
func (c *Client) IncomingCalls(ctx context.Context, file string, pos Position) json.RawMessage {
	return c.callHierarchy(ctx, "callHierarchy/incomingCalls", file, pos)
}

// OutgoingCalls returns callHierarchy/outgoingCalls for the first call-hierarchy
// item at the position, or nil. Ports callHierarchyRequest (lsp.ts:459-474) for
// the outgoing direction.
func (c *Client) OutgoingCalls(ctx context.Context, file string, pos Position) json.RawMessage {
	return c.callHierarchy(ctx, "callHierarchy/outgoingCalls", file, pos)
}

// callHierarchy runs prepareCallHierarchy then issues the directional call on the
// first item. Returns nil when there is no item or the request fails. Ports
// callHierarchyRequest (lsp.ts:459-474): item is items[0].
func (c *Client) callHierarchy(ctx context.Context, direction, file string, pos Position) json.RawMessage {
	prep := c.textDocumentRequest(ctx, "textDocument/prepareCallHierarchy", file, pos, nil)
	if prep == nil {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(prep, &items); err != nil || len(items) == 0 {
		return nil
	}
	return c.call(ctx, direction, map[string]any{"item": items[0]})
}

// workspaceSymbolLimit caps workspace/symbol results per client (lsp.ts:441).
const workspaceSymbolLimit = 10

// workspaceSymbolKinds is the curated SymbolKind allow-list for workspaceSymbol
// (lsp.ts:90-99): Class, Function, Method, Interface, Variable, Constant, Struct,
// Enum. Values are the LSP 3.17 SymbolKind numbers.
var workspaceSymbolKinds = map[int]bool{
	5:  true, // Class
	6:  true, // Method
	10: true, // Enum
	11: true, // Interface
	12: true, // Function
	13: true, // Variable
	14: true, // Constant
	23: true, // Struct
}

// symbolKindIncluded reports whether a raw workspace symbol's kind is in the
// curated allow-list (lsp.ts:441 `kinds.includes(x.kind)`).
func symbolKindIncluded(raw json.RawMessage) bool {
	var s struct {
		Kind int `json:"kind"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return false
	}
	return workspaceSymbolKinds[s.Kind]
}
