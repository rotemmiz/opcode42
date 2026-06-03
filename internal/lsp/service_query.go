package lsp

import (
	"context"
	"encoding/json"

	"go.lsp.dev/uri"
)

// Query operations on the service. Each mirrors a method on opencode's LSP
// service (lsp.ts:381-482). The flow is: spawn the matching servers for the file
// (run/getClients in opencode triggers lazy spawn), then issue the request on
// every matching client and aggregate. Results are raw json.RawMessage so the
// exact LSP wire shape flows through to the tool's JSON output unchanged
// (opencode JSON.stringifies the raw responses).
//
// Line/character are 0-based here — the lsp tool converts the agent's 1-based
// values before calling these (tool/lsp.go; opencode tool/lsp.ts:64).
//
// opencode's aggregation differs per op:
//   - definition/references/implementation/documentSymbol/prepareCallHierarchy/
//     incoming/outgoing: results.flat().filter(Boolean) — drop nil and flatten
//     one level of array results.
//   - hover: returns the per-client results verbatim (no flat/filter), so a
//     position with no hover yields [null] per client.
//   - workspaceSymbol: runAll over every client, already-filtered+capped, flat.

// runOnFile spawns the matching servers for file (EnsureClients) and runs fn on
// each matching client, collecting the non-nil raw results. It is the analog of
// opencode's run(file, fn) (lsp.ts:301-304): getClients (lazy spawn) then map.
func (s *Service) runOnFile(file string, fn func(c *Client) json.RawMessage) []json.RawMessage {
	_, _ = s.EnsureClients(file)
	var out []json.RawMessage
	for _, c := range s.clientsForFile(file) {
		if raw := fn(c); raw != nil {
			out = append(out, raw)
		}
	}
	return out
}

// flatFilter flattens one level (a result that is itself a JSON array is spread)
// and drops JSON nulls. Ports opencode's `results.flat().filter(Boolean)`
// (lsp.ts:401,414,426,…): each client's result may be a single object or an
// array, and the outer list is flattened before filtering falsy entries.
func flatFilter(results []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(results))
	for _, r := range results {
		if isJSONNull(r) {
			continue
		}
		// A result that is a JSON array is spread one level (Array.flat()).
		if len(r) > 0 && r[0] == '[' {
			var items []json.RawMessage
			if err := json.Unmarshal(r, &items); err == nil {
				for _, it := range items {
					if !isJSONNull(it) {
						out = append(out, it)
					}
				}
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// isJSONNull reports whether raw is the JSON literal null (the falsy value
// filter(Boolean) drops). Empty input is treated as null too.
func isJSONNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}

// Hover returns each matching client's textDocument/hover result verbatim (no
// flatten/filter, matching lsp.ts:381-390 which returns run()'s array directly).
func (s *Service) Hover(file string, line, character int) []json.RawMessage {
	ctx := context.Background()
	pos := Position{Line: line, Character: character}
	out := make([]json.RawMessage, 0)
	_, _ = s.EnsureClients(file)
	for _, c := range s.clientsForFile(file) {
		out = append(out, c.Hover(ctx, file, pos))
	}
	return out
}

// Definition returns the flattened, non-nil textDocument/definition results.
// Ports LSP.definition (lsp.ts:392-402).
func (s *Service) Definition(file string, line, character int) []json.RawMessage {
	ctx := context.Background()
	pos := Position{Line: line, Character: character}
	return flatFilter(s.runOnFile(file, func(c *Client) json.RawMessage {
		return c.Definition(ctx, file, pos)
	}))
}

// References returns the flattened, non-nil textDocument/references results.
// Ports LSP.references (lsp.ts:404-415).
func (s *Service) References(file string, line, character int) []json.RawMessage {
	ctx := context.Background()
	pos := Position{Line: line, Character: character}
	return flatFilter(s.runOnFile(file, func(c *Client) json.RawMessage {
		return c.References(ctx, file, pos)
	}))
}

// Implementation returns the flattened, non-nil textDocument/implementation
// results. Ports LSP.implementation (lsp.ts:417-427).
func (s *Service) Implementation(file string, line, character int) []json.RawMessage {
	ctx := context.Background()
	pos := Position{Line: line, Character: character}
	return flatFilter(s.runOnFile(file, func(c *Client) json.RawMessage {
		return c.Implementation(ctx, file, pos)
	}))
}

// DocumentSymbol returns the flattened, non-nil textDocument/documentSymbol
// results for a file:// URI. Ports LSP.documentSymbol (lsp.ts:429-435): the URI
// is converted to a file path to select the matching servers.
func (s *Service) DocumentSymbol(uriStr string) []json.RawMessage {
	ctx := context.Background()
	file := uri.New(uriStr).Filename()
	return flatFilter(s.runOnFile(file, func(c *Client) json.RawMessage {
		return c.DocumentSymbol(ctx, uriStr)
	}))
}

// WorkspaceSymbol returns the curated, capped workspace/symbol results from every
// running client. Ports LSP.workspaceSymbol (lsp.ts:437-445): runAll (no file →
// no spawn), each client's results already kind-filtered and capped at 10.
func (s *Service) WorkspaceSymbol(query string) []json.RawMessage {
	ctx := context.Background()
	out := make([]json.RawMessage, 0)
	for _, c := range s.allClients() {
		out = append(out, c.WorkspaceSymbol(ctx, query)...)
	}
	return out
}

// PrepareCallHierarchy returns the flattened, non-nil
// textDocument/prepareCallHierarchy results. Ports LSP.prepareCallHierarchy
// (lsp.ts:447-457).
func (s *Service) PrepareCallHierarchy(file string, line, character int) []json.RawMessage {
	ctx := context.Background()
	pos := Position{Line: line, Character: character}
	return flatFilter(s.runOnFile(file, func(c *Client) json.RawMessage {
		return c.PrepareCallHierarchy(ctx, file, pos)
	}))
}

// IncomingCalls returns the flattened, non-nil callHierarchy/incomingCalls
// results. Ports LSP.incomingCalls (lsp.ts:476-478).
func (s *Service) IncomingCalls(file string, line, character int) []json.RawMessage {
	ctx := context.Background()
	pos := Position{Line: line, Character: character}
	return flatFilter(s.runOnFile(file, func(c *Client) json.RawMessage {
		return c.IncomingCalls(ctx, file, pos)
	}))
}

// OutgoingCalls returns the flattened, non-nil callHierarchy/outgoingCalls
// results. Ports LSP.outgoingCalls (lsp.ts:480-482).
func (s *Service) OutgoingCalls(file string, line, character int) []json.RawMessage {
	ctx := context.Background()
	pos := Position{Line: line, Character: character}
	return flatFilter(s.runOnFile(file, func(c *Client) json.RawMessage {
		return c.OutgoingCalls(ctx, file, pos)
	}))
}
