# Opcode42 LSP — Language Server Protocol client (plan 03, Phase C)

## Context

Plan 03 has two halves: **MCP** (done — config/connect/status/tools merged into the loop, PRs #59/#62) and **LSP**, which is entirely unstarted. LSP gives the agent two things opencode users rely on: (1) **post-edit diagnostics** — after the agent edits a file, compiler/linter errors are appended to the tool output so the model fixes them on the next turn (the high-value feature); (2) **`GET /find/symbol`** — workspace symbol search.

This is a faithful **re-port** of opencode's LSP (TS → Go), not a code copy. The integration surface is **identical to the MCP work we just shipped** (per-instance manager on `instance.Context`, lazy, config-parsed, closed on dispose; `tool.Context` injection; a structural interface for the executor; a `GET /lsp` status endpoint), so we reuse our own proven pattern.

### Reuse from opencode (assessment)
- **Free from `go.lsp.dev`** (vetted lib, CLAUDE.md): all LSP protocol types + stdio JSON-RPC framing (`go.lsp.dev/protocol` + `go.lsp.dev/jsonrpc2`). Replaces opencode's `vscode-jsonrpc` + hand-rolled types.
- **Portable as spec/data** (TS→Go): the config schema (`bool | {command,extensions,disabled,env,initialization}`), the built-in **server table** (id/extensions/root-markers/spawn cmd — trimmed to PATH-spawnable first), `NearestRoot` walk, diagnostic formatting (`ERROR [L:C] msg`, cap ~20), `/find/symbol` kind-filter + top-10, the `lsp.client.diagnostics` SSE shape, and the `touchFile → report` tool-integration pattern.
- **Reimplement in Go** (TS-specific runtime, mirror MCP): client lifecycle, push+pull diagnostics + dedup/debounce, bus publishing, tool wiring, HTTP handlers.
- **Deferred** (biggest TS-specific chunk): opencode's per-language **binary auto-download** (`go install`, `gem install`, GitHub-release fetch + platform/arch extraction). Opcode42 uses a server only if its binary is on PATH; otherwise reports it unavailable. Logged as a divergence.

## Approach — phased (each phase = one PR through the full review/CI gate)

Mirror the MCP package layout: new `internal/lsp` with `config.go`, `server.go`, `client.go`, `manager.go`.

### PR-1 — LSP foundation (config + client + per-instance manager + `GET /lsp`)
- **`internal/lsp/config.go`**: `ParseConfig(cfg)` reads `cfg["lsp"]` → `bool | map[string]Entry` (mirror `internal/mcp/config.go`). `true`/absent ⇒ all built-ins; `false` ⇒ none; per-entry `{command,extensions,disabled,env,initialization}`. Custom (non-built-in) entries require `extensions`.
- **`internal/lsp/server.go`**: `Server{ID, Extensions, RootMarkers, ExcludeMarkers, Command}` + `NearestRoot(file, dir)` (walk up to the instance dir, first marker wins; exclude-aware — ported from opencode `lsp/server.ts:34-56`). Built-in table = **PATH-spawnable, no-download** set: `gopls`, `rust-analyzer`, `deno`, `clangd`, `pyright`, `zls`, plus the other fixed-command PATH-only servers (dart, gleam, sourcekit-lsp, nixd, terraform-ls, …). Defer `typescript`/`eslint`/`vue` (npm resolution) and all auto-download servers.
- **`internal/lsp/client.go`**: spawn the binary (stdio), `initialize`/`initialized` handshake via `go.lsp.dev/jsonrpc2` + `protocol`, `didOpen`/`didChange`, receive `publishDiagnostics` (push only this PR; pull deferred), `workspace/symbol`. Binary-not-on-PATH ⇒ status `failed`.
- **`internal/lsp/manager.go`**: per-instance `Manager` (mirror `mcp.Manager`) — lazy: on first use, given a file, match extension → `NearestRoot` → spawn-or-reuse a client keyed by `(root, serverID)`, with a `broken` set + in-flight `spawning` dedup. `Status()` (connected/disabled/failed), `Diagnostics()`, `Symbols(query)`, `TouchFile(path)`, `Close()`.
- **Wiring** (mirror MCP exactly): `instance.Context.LSP *lsp.Manager` built from config in `instance.Get`, closed in `DisposeAll` (`internal/instance/instance.go`); `GET /lsp` status handler (new `internal/server/lsp_handlers.go`, mirror `mcp_handlers.go`) returning the openapi `LSPStatus` map.
- **Tests**: config parse; `NearestRoot`; client against a **stub LSP server** (a tiny in-test stdio JSON-RPC server that answers `initialize` + emits a `publishDiagnostics`), so no real toolchain is needed in CI; `GET /lsp` status (empty + a disabled/failed entry from config).

### PR-2 — Post-edit diagnostics in tool results (the high-value feature)
- After `edit`/`write`/`patch` (`internal/engine/tool/files.go`), call the instance LSP manager's `TouchFile(path)` then `Diagnostics(path)`, format (`ERROR [L:C] msg`, cap ~20), and append to `Result.Output` (+ raw in `Result.Metadata["diagnostics"]`). Inject the LSP manager into `tool.Context` (new field, mirror `Questioner`/`MCP`), wired through `engine.Config` + the executor + `buildEngine` (the exact path used for MCP).
- Emit the `lsp.client.diagnostics` SSE event on the instance bus when diagnostics update.
- **Tests**: stub-LSP-backed engine test — an edit produces a tool output containing the stubbed diagnostic; SSE event emitted.

### PR-3 — `GET /find/symbol`
- New handler (extend `internal/server/find_handlers.go`): `query` param → `inst.LSP.Symbols(query)` (`workspace/symbol`, filter to Class/Function/Method/Interface/Variable/Constant/Struct/Enum, top 10), return the openapi `Symbol[]` shape. Currently 501 via the spec loop.
- **Tests**: stub-LSP returning symbols → endpoint shape; empty `[]` when no clients.

### Later (deferred, logged in known-divergences)
Pull diagnostics + dedup/debounce; `typescript`/`eslint`/`vue` (npm resolution); per-language binary **auto-download**; the rest of the 35-server table; hover/definition/references query tools; `disableLspDownload`/`disableLsp` flags.

## Critical files
- New: `internal/lsp/{config.go,server.go,client.go,manager.go}` (+ tests), `internal/server/lsp_handlers.go`.
- Edit: `internal/instance/instance.go` (add `LSP` field; build in `Get`; `Close` in `DisposeAll` — mirror `MCP` at the existing anchors), `internal/server/server.go` (register `/lsp`), `go.mod` (add `go.lsp.dev/protocol` + `go.lsp.dev/jsonrpc2`, pinned).
- PR-2/3 edits: `internal/engine/tool/files.go`, `internal/engine/tool/tool.go` (Context field), `internal/engine/engine.go` + `loop.go` + `registry/executor.go` (inject, mirror MCP), `internal/server/prompt_handlers.go` (buildEngine), `internal/server/find_handlers.go` (/find/symbol).
- `conformance/known-divergences.json`: add an `lsp` entry each PR.

## Reused existing code/patterns
- `internal/mcp/{config.go,manager.go}` and `internal/server/mcp_handlers.go` — the template for config parse, lazy per-instance manager (incl. the Close-vs-connect locking we already got right), and the status endpoint.
- `internal/instance/instance.go:Get/DisposeAll` + `mcpServers(directory)` — the per-instance build/dispose hooks.
- `internal/config.Load` — the merged config; `internal/worktree.Root` — for `NearestRoot`'s stop dir.
- The `tool.Context` injection chain (`Questioner`/`Subagent`/`Skiller`/`MCP`) — the exact pattern for delivering the per-instance LSP manager to tools.

## Verification
- CI-mimic gate each PR: `go build`/`vet`, `gofmt -l`, `golangci-lint run`, `go test ./... -race`, `make gen` + clean gen-diff, `scripts/run-conformance.sh self`.
- Unit/integration via an **in-test stub LSP server** (stdio JSON-RPC answering `initialize` + emitting `publishDiagnostics`/`workspace/symbol`) — deterministic, no real toolchain in CI.
- Live-smoke (local, optional, needs a real server on PATH): point `opcoded` at a Go project with `gopls` installed; `GET /lsp` shows `gopls` connected; an edit that introduces a compile error surfaces the diagnostic in the tool output; `GET /find/symbol?query=...` returns symbols.
- Append human-verify items to `tasks/verify.md`.
