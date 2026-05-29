# Forge

  Forge is a **ground-up, interop-first alternative to opencode**: a **Go daemon** that is
  **wire-compatible** with opencode's HTTP+SSE+WebSocket API and **ecosystem-compatible** with its
  config/resource formats (MCP, LSP, `.opencode/agent`, `AGENTS.md`, commands, skills, providers,
  plugins). **Mobile (Android) is the primary client.** A Go/Bubble Tea TUI is a dogfood vehicle.

  This is a clean rewrite. An earlier Rust attempt was intentionally abandoned — do not resurrect it.

  ## The plans are the source of truth
  Detailed, source-grounded engineering plans live in `plans/`. Start at `plans/00-masterplan.md`
  (vision + the frozen wire contract + sequencing), then read the specific component plan before
  building it. **Do not re-architect** — these plans were reviewed and grounded against opencode.
  Update a plan only if it contradicts reality, and say so explicitly.

  ## Reference codebase
  opencode is checked out at `/Users/rotemmiz/git/opencode`. It is the compatibility reference —
  validate every wire/config claim against it (cite `file:line`). Frozen contract:
  `/Users/rotemmiz/git/opencode/packages/sdk/openapi.json` (~113 endpoints).

  ## Build order (Phase A)
  1. `plans/12-test-compatibility.md` — conformance harness. Record real opencode behavior; dual-run diff. Build first; it's the correctness gate.
  2. `plans/01-daemon-core.md` — Go transport, SQLite, auth, per-directory instance routing, SSE bus.
  3. `plans/07-client-mobile.md` — Android app; can start against the real opencode daemon before the Go daemon is ready.

  Then B (`02-agent-engine`), C (`03`/`04`), D (`05` plugin-host, `13` remote-ops, `08` TUI).

  ## Non-negotiables
  - **Wire-compat by default.** Match opencode's endpoints, the SSE `{ id, type, properties }` shape, PTY framing (`0x00 + UTF-8 JSON {cursor}`), Basic/`?auth_token=` auth, `x-opencode-directory` routing. Log
  intentional divergences in a known-divergence registry (plan 12).
  - **No fabricated numbers.** Perf multipliers vs opencode are *unmeasured targets* until both daemons are run head-to-head (plan 11, W0 = measure opencode first).
  - **Go runtime, single binary.** Libs vetted in the plans (chi/net-http, coder/websocket, modernc.org/sqlite, oapi-codegen, mark3labs/mcp-go, go.lsp.dev).