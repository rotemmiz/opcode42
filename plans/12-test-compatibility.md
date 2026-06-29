# Plan 12 — Conformance Harness: Wire Compatibility Testing

> Scope: the primary correctness gate for Opcode42's interoperability claim.
> A Opcode42 daemon that passes this harness is behaviorally indistinguishable
> from an opencode daemon to any compliant client.
> "Interop is both the product goal and the development methodology." — plan 00.

---

## Context

Opcode42's central proposition is **wire compatibility** with opencode's HTTP+SSE+WS API.
This plan defines the full conformance strategy: capturing the contract from
`packages/sdk/openapi.json`, recording the empirical SSE event catalog from a real
opencode session, running a client-driven scenario suite against **both** daemons and
diffing results byte-for-byte (after normalizing volatile fields), and using
opencode's own unmodified clients as the final acceptance test.

Interop means:
1. Every endpoint in `openapi.json` (113 paths) responds with the correct schema.
2. SSE event streams match type-for-type and field-for-field.
3. PTY WebSocket framing is identical.
4. Auth and directory routing behave identically.
5. opencode's unmodified TUI and web app work against the Opcode42 daemon.

### Key source references
- Wire contract: `packages/sdk/openapi.json` (22 230 lines, 113 path entries)
- SSE handler (shows eager-subscribe pattern, event shape): `packages/opencode/src/server/routes/instance/httpapi/handlers/event.ts:21-53`
- Auth middleware: `packages/opencode/src/server/routes/instance/httpapi/middleware/authorization.ts`
- Directory routing: `packages/opencode/src/server/routes/instance/httpapi/middleware/workspace-routing.ts:86-88`
- http-recorder package: `packages/http-recorder/` — record/replay HTTP+WS cassettes
- Cassette schema: `packages/http-recorder/src/schema.ts` — `HttpInteraction` + `WebSocketInteraction`
- Cassette service: `packages/http-recorder/src/cassette.ts` — file-backed cassette at `test/fixtures/recordings/<name>.json`
- Effect layer: `packages/http-recorder/src/effect.ts` — `cassetteLayer` / `recordingLayer`
- WebSocket recorder: `packages/http-recorder/src/websocket.ts` — `makeWebSocketExecutor`

---

## (a) Contract Capture and Spec-Drift Gate

### Canonical spec
The canonical wire contract is `packages/sdk/openapi.json`, vendored into the Opcode42
repo at `conformance/openapi-reference.json`. This file is the source of truth.
**Never modify it manually.** Update it only by pulling from opencode and running
the drift check.

### Opcode42 self-emits its spec
The Opcode42 daemon exposes `GET /openapi.json` (or built in to the oapi-codegen
server stubs). The emitted spec must match the vendored reference.

### Drift detection CI gate

```bash
# scripts/check-spec-drift.sh
opcode42_url=http://localhost:4096
curl -sf $opcode42_url/openapi.json > /tmp/opcode42-spec.json
npx openapi-diff conformance/openapi-reference.json /tmp/opcode42-spec.json \
  --fail-on-incompatible
```

Run as a CI step on every PR. Any breaking difference (missing path, changed
request/response schema, new required field, changed status code) **fails the build**.

Non-breaking additions (Opcode42 adds an endpoint not in the reference) are allowed
but logged as warnings. A `conformance/known-additions.json` registry tracks
intentional additions.

### Schema coverage tracking

```go
// conformance/coverage_test.go
// For every path + method in openapi-reference.json, verify that
// the Opcode42 server returns a non-500 status when called with a valid
// request. Generates conformance/coverage-report.json.
func TestSpecCoverage(t *testing.T) {
    // Uses oapi-codegen stubs to enumerate all operations.
    // For each: construct minimal valid request, call Opcode42, assert status != 5xx.
}
```

---

## (b) SSE Event Catalog Recording with http-recorder

### Investigation: `packages/http-recorder`

The `packages/http-recorder` package (`packages/http-recorder/README.md`,
`packages/http-recorder/src/cassette.ts:7`) is opencode's own VCR library for
recording HTTP and WebSocket traffic as versioned JSON cassettes. It is already
used in opencode's test suite to capture real LLM API interactions.

**Key capabilities relevant to the conformance harness:**

- **Cassette format** (`schema.ts:22-48`): JSON file at
  `test/fixtures/recordings/<name>.json` with `version: 1`, `metadata`, and
  `interactions: []`. Each interaction is tagged `transport: "http"` or
  `transport: "websocket"`.
- **HTTP interactions** (`schema.ts:22-26`): record full request (method, url,
  headers, body) and response (status, headers, body, bodyEncoding).
- **WebSocket interactions** (`schema.ts:36-44`): record the open URL+headers,
  all client frames, and all server frames.
- **Secret safety** (`cassette.ts:58-59`): refuses to write cassettes containing
  secret patterns (API keys, Bearer tokens, `sk-ant-…`). Essential for committing
  SSE recordings that contain session data but must not contain auth credentials.
- **Modes** (`effect.ts:20`): `auto` (record if missing, replay if present, CI forces
  replay), `record`, `replay`, `passthrough`.
- **WebSocket executor** (`websocket.ts:72`): `makeWebSocketExecutor` wraps a live
  WebSocket connection with record/replay behavior, capturing open frame +
  client/server message streams.

**How we use it for SSE catalog recording:**

SSE is HTTP (streaming response). The http-recorder captures it as an
`HttpInteraction` with `transport: "http"`. The SSE body is captured in full as
the `response.body` string — it is a sequence of `data: {...}\n\n` lines.

**Recording procedure:**
1. Start a real opencode daemon (`opencode serve --port 4096`).
2. Run the recording script (`conformance/record.ts`) using the http-recorder's
   `recordingLayer` (`effect.ts:60-135`):
   - Connect to `GET /event` SSE.
   - Run each scenario (create session, submit prompt, etc.).
   - Allow recording to complete.
3. Cassettes are written to `conformance/cassettes/<scenario-name>.json`.
4. Sensitive fields (API keys, auth tokens) are redacted by the default
   `Redactor.defaults()` (`effect.ts:69`).
5. Commit the cassettes to the Opcode42 repo.

**WebSocket (PTY) recording** uses `makeWebSocketExecutor` (`websocket.ts:72`)
to capture the full PTY WebSocket session: open URL, resize control frames, input
frames, output frames.

**Replay mode:** In CI, the http-recorder plays back recorded cassettes instead
of hitting a live daemon. This means the conformance tests are deterministic in
CI even without a running opencode daemon.

**Limitations:**
- http-recorder is TypeScript/Effect — not directly callable from Go. For the
  Go-side conformance runner, we either: (a) call the recording script via
  `os/exec` (Node/Bun), or (b) port the cassette reader to Go (cassette format is
  simple JSON — see schema below).
- The cassette cursor is sequential (`recorder.ts:27` — `makeReplayState` advances
  a cursor for each request). Tests that send requests in a different order from
  the recording will fail. Re-record if scenario order changes.

**Cassette JSON schema (for Go reader):**
```json
{
  "version": 1,
  "metadata": { "name": "...", "recordedAt": "..." },
  "interactions": [
    {
      "transport": "http",
      "request":  { "method": "GET", "url": "...", "headers": {...}, "body": "" },
      "response": { "status": 200, "headers": {...}, "body": "data: {...}\n\n..." }
    },
    {
      "transport": "websocket",
      "open": { "url": "...", "headers": {...} },
      "client": [{ "kind": "text", "body": "{...}" }],
      "server": [{ "kind": "text", "body": "..." }]
    }
  ]
}
```

A Go package `conformance/cassette/` reads this format and provides typed access
to interactions. See `schema.ts:3-68` for the complete field definitions.

---

## (c) Scenario Conformance Suite

### Design

The scenario suite is a Go test program (`conformance/suite_test.go`) that:
1. Accepts a `--target` flag: `opencode` or `opcode42` (daemon URL).
2. Runs every scenario against the target.
3. Records request/response pairs to a result file.
4. When run against both targets, diffs the result files using the normalizer.

```bash
# Record against opencode (truth)
go test ./conformance/... --target=http://localhost:4096 --record --out=results/opencode.json

# Record against Opcode42
go test ./conformance/... --target=http://localhost:4097 --record --out=results/opcode42.json

# Diff
go run ./conformance/cmd/diff results/opencode.json results/opcode42.json
```

### Scenario list

All scenarios must pass with zero structural differences after normalization.

#### Core session lifecycle
1. **session-create-list**: `POST /session` → `GET /session` → assert session in list.
2. **session-get-delete**: create, get by ID, delete, confirm 404.
3. **session-fork**: create session, fork it, assert `parentID` set, independent histories.
4. **session-children**: fork twice, `GET /session/:id/children` returns both.

#### Prompt and SSE event stream
5. **prompt-text-only**: `POST /session/:id/prompt_async` with mock provider returning
   text; assert SSE sequence:
   ```
   server.connected
   message.updated.1 (role=user)
   message.updated.1 (role=assistant, pending)
   part.updated (text, delta sequence)
   message.updated.1 (role=assistant, completed)
   ```
6. **prompt-tool-call**: mock provider calls one built-in tool; assert SSE sequence:
   ```
   part.updated (tool, status=pending)
   part.updated (tool, status=running)
   part.updated (tool, status=completed)
   message.updated.1 (completed)
   ```
7. **prompt-multi-turn**: three-turn conversation; assert message ordering and
   `parentID` chain.

#### Permission round-trip
8. **permission-asked-approve**: mock provider calls bash tool; ruleset triggers ask;
   assert `permission.asked` event; send `POST /session/:id/permissions/:id
   {"response":"once"}`; assert `permission.replied` event and tool completes.
9. **permission-asked-reject**: same but respond with `"reject"`; assert tool part
   `status=error`; assert `permission.replied`.
10. **permission-always**: respond with `"always"`; second call to same tool is
    auto-approved; no new `permission.asked` event.

#### Revert and diff
11. **session-revert**: prompt that writes a file; `POST /session/:id/revert`;
    assert `GET /session/:id/diff` returns the expected diff.
12. **session-unrevert**: revert then unrevert; file content restored.

#### PTY connect and framing
13. **pty-create-connect**: `POST /pty`; WebSocket connect to `/pty/:id/connect`;
    send control frame `0x00 + JSON({"cursor":{"rows":24,"cols":80}})`;
    assert server echoes output.
    Reference: plan 00 PTY WS framing spec.
14. **pty-resize**: send second control frame with different dimensions; assert
    no error.
15. **pty-input-output**: send shell command via text frame; assert output frames
    contain expected text.
16. **pty-exit**: close WebSocket; assert `pty.exited` SSE event published.

#### SSE reconnect and replay (`/sync/*`)
17. **sse-reconnect**: connect to `/event`; disconnect; reconnect; assert
    `server.connected` event on reconnect.
18. **sync-replay**: `GET /sync/replay?from=<cursor>` returns missed events.
    (Best-effort; mark as skip if not implemented in Phase A.)
19. **sse-heartbeat**: no activity for 10s; assert `server.heartbeat` event.

#### Auth and routing
20. **auth-basic**: valid `Authorization: Basic` header passes; invalid → 401
    with `WWW-Authenticate: Basic realm="Secure Area"`.
    Reference: `authorization.ts:11`.
21. **auth-token-query**: `?auth_token=<base64>` equivalent to Basic Auth.
    Reference: `authorization.ts:82-84`.
22. **auth-pty-ticket**: PTY connect with valid one-time ticket bypasses Basic Auth.
    Reference: `authorization.ts:147`.
23. **directory-header**: `x-opencode-directory: /path/to/project` routes to correct
    instance. Reference: `workspace-routing.ts:87`.
24. **directory-query**: `?directory=/path/to/project` equivalent to header.
    Reference: `workspace-routing.ts:87`.
25. **directory-default**: no header or query param → uses server's cwd.

#### Question API
26. **question-asked**: agent triggers a question (`Question.ask`); assert
    `question.asked` SSE event; `POST /question/:id/reply`; assert `question.replied`.
27. **question-rejected**: close connection without replying; assert `question.rejected`.

#### MCP integration
28. **mcp-server-list**: configure echo MCP server; `GET /mcp` lists it as connected.
29. **mcp-tool-call**: prompt triggers MCP tool; assert `part.updated` with MCP
    tool name and result.

#### Config and provider
30. **config-get**: `GET /config` → valid config JSON.
31. **provider-list**: `GET /provider` → list of configured providers.

---

## (d) Dual-Run Diffing Methodology

### Normalization

Before diffing, strip all volatile fields that are legitimately different between
two runs (ULIDs, timestamps, PIDs, filesystem paths):

```go
// conformance/normalize/normalize.go
type Normalizer struct {
    PathReplacements map[string]string  // absolute paths → "<path>"
}

func (n *Normalizer) NormalizeEvent(event map[string]any) map[string]any {
    // Replace id fields that are ULIDs
    replaceULID(event, "id")
    replaceULID(event, "sessionID")
    replaceULID(event, "messageID")
    replaceULID(event, "partID")
    replaceULID(event, "permissionID")
    replaceULID(event, "questionID")
    // Replace timestamps
    replaceTimestamp(event, "timestamp")
    replaceTimestamp(event, "createdAt")
    replaceTimestamp(event, "time.created")
    replaceTimestamp(event, "time.completed")
    replaceTimestamp(event, "time.start")
    replaceTimestamp(event, "time.end")
    // Replace absolute paths
    n.replacePaths(event)
    // Sort object keys for canonical JSON
    return canonicalize(event)
}
```

**Fields that are NOT normalized** (structural differences = test failures):
- `type` (event type string)
- `role` (user/assistant)
- `status` (pending/running/completed/error)
- `tool` name
- `output` text (tool results)
- HTTP status codes
- Response body schemas (field names and nesting)

### Diff output format

```
SCENARIO: prompt-tool-call
STEP 3: SSE event #7
  EXPECTED (opencode): {"type":"part.updated","properties":{"type":"tool","state":{"status":"completed",...}}}
  ACTUAL   (opcode42):    {"type":"part.updated","properties":{"type":"tool","state":{"status":"running",...}}}
  DIFF: properties.state.status: "completed" != "running"

SUMMARY: 1 failure in 1 scenario; 29 scenarios passed.
```

### Automated dual-run in CI

```yaml
# .github/workflows/conformance.yml
jobs:
  conformance:
    runs-on: ubuntu-latest
    services:
      opencode:
        image: ghcr.io/sst/opencode:pinned-tag
        ports: ["4096:4096"]
    steps:
      - name: Build Opcode42
        run: make build
      - name: Start Opcode42 daemon
        run: ./opcoded --port 4097 &
      - name: Run conformance suite against opencode
        run: go test ./conformance/... --target=http://localhost:4096 --out=results/opencode.json
      - name: Run conformance suite against Opcode42
        run: go test ./conformance/... --target=http://localhost:4097 --out=results/opcode42.json
      - name: Diff results
        run: go run ./conformance/cmd/diff results/opencode.json results/opcode42.json
```

The diff step exits non-zero on any structural difference not in the
known-divergence registry (see below).

---

## opencode Clients Against Opcode42 (Acceptance)

This is the strongest form of the interop proof. Run opencode's own unmodified
clients against the Opcode42 daemon and assert they work.

### Test 1: opencode TUI attach
```bash
opcoded --port 4097 &
opencode attach http://localhost:4097
```
Assert: TUI renders session list; typing a prompt and submitting produces a
response; tool calls show tool bubbles; permission dialogs appear correctly.
This is a manual test today; automate with `vhs` in Phase D.

### Test 2: opencode web app
```bash
opcoded --port 4097 &
# Open packages/web in browser, point to http://localhost:4097
```
Assert: session list loads; create session; submit prompt; stream renders.
Automate with Playwright in Phase D.

### Test 3: opencode SDK (TypeScript)
```typescript
// conformance/opencode-sdk-test.ts
import { createOpencodeClient } from "@opencode-ai/sdk"
const client = createOpencodeClient({ baseUrl: "http://localhost:4097" })
await client.global.health()
const session = await client.session.create({ ... })
// ... full CRUD + prompt flow
```
Run with `bun test conformance/opencode-sdk-test.ts`. This exercises the exact
SDK shapes against Opcode42. Failures here indicate a schema mismatch.

---

## PTY, Auth, and Routing Conformance

### PTY framing conformance
The PTY WebSocket framing is not in the OpenAPI spec (WS is described but framing
is not formally specified). The cassette records the raw frames:

From `packages/http-recorder/src/websocket.ts:43-48`, frames are stored as:
```json
{ "kind": "text", "body": "..." }
{ "kind": "binary", "body": "<base64>", "bodyEncoding": "base64" }
```

The PTY conformance test verifies:
- Control frame: first byte `0x00` followed by JSON `{"cursor": {...}}`.
  Opcode42 must produce the same frame structure when a resize event occurs.
- Data frames: UTF-8 text chunks. Maximum chunk size matches 64KB.
- Output buffering: 2MB total buffer; overflow behavior (Opcode42 must drop or error
  consistently with opencode).

Reference: plan 00 PTY WS framing spec. Validate against actual cassette recordings.

### Auth conformance
Scenario 20-22 above cover the auth matrix. Additional edge cases:
- Empty username + empty password with auth disabled → 200 (no auth required).
- Malformed base64 in `auth_token` → 401 (not 500).
- `Authorization: Basic` with wrong password → 401 with `WWW-Authenticate` header.

### Routing conformance
Scenarios 23-25 cover directory routing. Additional:
- `x-opencode-directory` with base64-encoded path (v2 format) → decoded correctly.
  Reference: `workspace-routing.ts:87` reads the raw header value; encoding is
  the client's responsibility.
- Two concurrent clients with different `x-opencode-directory` values → each
  gets their own instance's events.

---

## CI Gating

The conformance suite is the **primary merge gate** for the `dev` branch:

```
Required status checks:
  conformance/spec-drift        (openapi diff)
  conformance/scenario-suite    (dual-run diff)
  conformance/sdk-test          (opencode TS SDK against Opcode42)
```

Phase A: only spec-drift and 10 core scenarios required.
Phase B: all 31 scenarios required; SDK test added.
Phase C: MCP/LSP scenarios added.
Phase D: PTY cassette conformance + opencode TUI attach (manual → automated).

Scenario failures that are in the known-divergence registry do not block merge
but are reported as warnings.

---

## Known-Divergence Registry

`conformance/known-divergences.json` — list of intentional, accepted differences:

```json
[
  {
    "scenario": "sync-replay",
    "phase": "A",
    "reason": "SSE replay via /sync/* not implemented in Phase A",
    "track": "https://github.com/opcode42/opcode42/issues/42"
  },
  {
    "scenario": "experimental-*",
    "phase": "A-C",
    "reason": "/experimental/* endpoints best-effort; not in conformance gate until Phase D"
  },
  {
    "scenario": "provider-oauth",
    "phase": "A-B",
    "reason": "OAuth provider auth flow deferred; /provider/:id/oauth/authorize not implemented"
  }
]
```

Any divergence not in this registry causes a CI failure. Adding to the registry
requires a PR with a tracking issue. The registry shrinks over time as features
are implemented.

---

## Review pass (2026-06-03) — what the gate actually proves; reconcile strictness

This is the plan every other plan's "conformance-green" exit criterion depends on, so its claims must
be exact.

**1. The spec-drift gate proves coverage, not behavior — say so.** Interop item #1 ("every endpoint
responds with the correct schema") is **not** verified by the drift gate. `GET /openapi.json` serves
the **embedded reference verbatim** (`internal/server/server.go:86`), so curling it and diffing
against the reference is reference-vs-reference. `scripts/check-spec-drift.sh` meaningfully checks
**which operations are registered** (coverage), not per-handler request/response shapes. **Per-handler
schema conformance is owned by the dual-run scenario suite**, not the drift gate. Reword §(a) so
"drift green" is never read as "schemas conform." (Same finding as plan 06; M10 — a handler-derived
spec — would close it.)

**2. Strictness policy — DECIDED (2026-06-03).** **Missing/changed field = FAIL; extra additive
field = WARNING and must be listed in `conformance/known-additions.json`.** This is the single
canonical policy (masterplan "Decisions locked" #2); it supersedes both this plan's earlier "any
divergence = fail" line and plan 02's "extra fields = warning." Implement the normalizer/differ to
this rule. The normalizer's exact volatile-field strip set (ids, timestamps, cursors, ports, absolute
paths, session/message/part ids) is the crux of the whole harness — enumerate it canonically in one
place.

**3. The doc's known-divergence example is stale.** The inline JSON here (`sync-replay`,
`experimental-*`, `provider-oauth`, fake issue URL, `phase:"A"`) does not match the **live**
`conformance/known-divergences.json`, whose entries are richer (`mcp`, `provider-auth`, `websearch`
with detailed `reason` + `track: "TODO: …"`). Update the example to the live schema so contributors
copy the right shape.

**4. Status: the gate is not yet complete or green.** Recording + normalize infra and some cassettes
exist, but the **automated dual-run scenario suite is incomplete** and **no end-to-end green baseline
has been established** (plan 02 M11 unrun). Until that lands, every plan that says "conformance-green"
as an exit criterion is blocked on this. This is the single highest-leverage unfinished item in the
whole suite — prioritize the dual-run runner + the canonical normalizer.

## Links to All Sibling Plans

- [00-masterplan](00-masterplan.md) — wire-compat as product goal and methodology
- [01-daemon-core](01-daemon-core.md) — HTTP transport, auth, SSE bus, SQLite
- [02-agent-engine](02-agent-engine.md) — prompt flow, tool loop, permissions
- [03-ecosystem-mcp-lsp](03-ecosystem-mcp-lsp.md) — MCP/LSP integration
- [04-ecosystem-resources](04-ecosystem-resources.md) — agents, rules, providers
- [05-plugin-host](05-plugin-host.md) — TS plugin sidecar
- [06-sdk-generation](06-sdk-generation.md) — Go + Kotlin/Swift SDKs from openapi.json
- [07-client-mobile](07-client-mobile.md) — Android client using opencode wire protocol
- [08-client-tui](08-client-tui.md) — Go TUI; first internal client for conformance testing
- [09-integration](09-integration.md) — component wiring; sequence diagrams
- [10-test-functional](10-test-functional.md) — functional correctness tests
- [11-test-performance](11-test-performance.md) — performance benchmarks
