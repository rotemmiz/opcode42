# Opcode42

  Opcode42 is a **ground-up, interop-first alternative to opencode**: a **Go daemon** that is
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

  ## Git workflow (every feature) — agent owns the PR end-to-end, until merged
  An agent assigned a feature/track is NOT done when the PR opens — it is done only when the PR is
  **reviewed clean, CI-green, and merged**. Run this whole loop yourself; never hand back a
  green-but-unmerged PR. Do not push unreviewed work to `main`.
  1. **Start fresh:** `git checkout main && git pull`, then branch (`git checkout -b <feature>`).
  2. **Build the feature**, committing as the work warrants (no `Co-Authored-By`).
  3. **Local pre-push gate** (fast self-check before spending CI): `go build ./...`, `go vet ./...`,
     `gofmt -l` (empty), `golangci-lint run`, `go test ./...`, `make gen` + `git diff --exit-code
     internal/api/gen/`, `scripts/run-conformance.sh self` (+ a dual-run gate for new endpoints).
     Then commit, `git push -u origin <feature>`, `gh pr create`.
  4. **Independent review loop.** Spin a SEPARATE review subagent (do NOT review your own diff
     inline) → read its findings → fix every blocking/should-fix item → push the review-round fixes →
     re-spin the review. Repeat until the review comes back **clean**.
  5. **Wait for green CI.** GitHub Actions runs on the PR — `gh pr checks <pr> --watch`. Fix any
     failure and re-push until all checks pass.
  6. **Merge** (`gh pr merge --squash`) once review is clean AND CI is green; if `main` moved, rebase
     first and re-run the gate. Then `git checkout main && git pull` to sync. The agent exits only
     after the merge succeeds.

  ## Non-negotiables
  - **Wire-compat by default.** Match opencode's endpoints, the SSE `{ id, type, properties }` shape, PTY framing (`0x00 + UTF-8 JSON {cursor}`), Basic/`?auth_token=` auth, `x-opencode-directory` routing. Log
  intentional divergences in a known-divergence registry (plan 12).
  - **No fabricated numbers.** Perf multipliers vs opencode are *unmeasured targets* until both daemons are run head-to-head (plan 11, W0 = measure opencode first).
  - **Go runtime, single binary.** Libs vetted in the plans (chi/net-http, coder/websocket, modernc.org/sqlite, oapi-codegen, mark3labs/mcp-go, go.lsp.dev).