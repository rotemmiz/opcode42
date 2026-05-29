# Plan 10 — Functional Test Strategy

> Scope: unit, integration, and component tests across all Forge subsystems.
> Covers Go unit tests, daemon integration tests, mock-LLM agent-loop tests,
> MCP/LSP ecosystem tests, mobile UI tests, and TUI smoke tests.
> The conformance harness (plan 12) is the complementary correctness gate for
> wire-level interop; this plan owns correctness of the Forge implementation itself.

---

## Context

opencode's test suite (`packages/opencode/test/`) provides a useful reference:
- One subdirectory per feature area (`session/`, `permission/`, `mcp/`, `agent/`).
- Three runner flavors: pure-logic `it.effect`, instance-scoped `it.instance`, real-I/O `it.live`.
- Fake boundary layers in `test/fake/*` (`ProviderTest.fake`, `SkillTest.empty`).
- Assertions against the event bus, not the UI.

Forge's analog uses Go's standard `testing` package with a three-tier structure that
mirrors this. The key design principle is the same: **test against the event bus and
stored state, not the transport layer**. HTTP/SSE tests exercise the transport but
assert against the same underlying state.

Reference: `packages/opencode/src/session/prompt.ts`, `processor.ts`,
`packages/opencode/src/permission/index.ts`, `packages/sdk/openapi.json`.

---

## Test Pyramid for Forge

```
           ┌────────────────────────┐
           │  Live / E2E (gated)    │  real LLM API, real MCP servers
           │  ~10 tests             │  skipped unless FORGE_LIVE_TESTS=1
           ├────────────────────────┤
           │  Daemon integration    │  spin daemon, hit HTTP/SSE, assert
           │  ~100 tests            │  mock LLM, mock MCP echo server
           ├────────────────────────┤
           │  Unit per package      │  pure Go, no I/O, table-driven
           │  ~400 tests            │  fast (<1ms each), always run
           └────────────────────────┘
```

---

## Unit Tests (per package)

All unit tests live in `_test.go` files alongside their package. No external
processes. Table-driven where there are more than three cases. Target: < 1ms per test.

### `internal/config`
- `TestConfigLoad`: parse valid TOML, assert all fields decoded.
- `TestConfigMerge`: global + project merge; project wins on conflict.
- `TestConfigValidation`: missing required provider key → error with field path.
- `TestConfigHotReload`: file watcher fires → config struct updated → subscriber notified.
- Fixtures: `testdata/config/valid.toml`, `testdata/config/project-override.toml`,
  `testdata/config/missing-provider.toml`.

### `internal/permission`
- `TestRulesetMatch`: pattern `bash:*` matches `bash:rm -rf`, rejects `read_file:*`.
- `TestRulesetMerge`: agent ruleset + session ruleset; session wins.
- `TestPermissionIDUnique`: 1 000 rapid IDs have no collisions (ULID property).
- `TestDoomLoopDetection`: three identical tool calls in a row → `doom_loop` permission
  asked. Reference: `processor.ts:424-448`.
- `TestPermissionReplyUnblock`: `Ask()` blocks until `Reply()` is called;
  `reject` reply returns `RejectedError`.
- Fixtures: `testdata/permission/rulesets.json`.

### `internal/agent` (message model)
- `TestMessageIDOrdering`: ascending ULID ordering for message and part IDs.
- `TestPartDeltaAccumulation`: sequence of text deltas assembles correct final text.
- `TestMessageToModelMessages`: `MessageWithParts` → LLM SDK message array; tool
  results formatted correctly; synthetic parts excluded from model context.
- `TestOrphanedInterruptedTool`: `isOrphanedInterruptedTool` recognizes the
  `status=error + metadata.interrupted=true` marker. Reference: `prompt.ts:84-88`.

### `internal/session` (store)
- `TestSessionCRUD`: create / get / update / delete round-trip against in-memory SQLite.
- `TestPartDeltaWrite`: `UpdatePartDelta` appends delta to correct part; concurrent
  deltas from multiple goroutines produce correct final text (no data races).
- `TestSessionBusyGuard`: concurrent prompts on same session → second returns `BusyError`.

### `internal/transport` (auth + routing)
- `TestBasicAuthDecode`: `Authorization: Basic dXNlcjpwYXNz` → `{user, pass}`.
- `TestAuthTokenQuery`: `?auth_token=dXNlcjpwYXNz` → same credential.
- `TestDirectoryResolution`: `x-opencode-directory` header → directory string;
  `?directory=` query param → directory string; neither → `process.cwd()` analog.
  Reference: `workspace-routing.ts:86-88`.
- `TestAuthRequired`: no credential when auth configured → 401 with `WWW-Authenticate`.
- `TestPTYTicketBypass`: PTY connect ticket in URL bypasses Basic Auth check.
  Reference: `authorization.ts:147-153`.

### `internal/tool`
- `TestToolInputValidation`: JSON Schema validation rejects bad input, returns
  structured error.
- `TestTruncation`: output exceeding max length is truncated with marker.
- `TestToolResultFormat`: `output` + `title` + `metadata` wrapped correctly for
  LLM context.

### `internal/mcp`
- `TestMCPStdioFrame`: write JSON-RPC request to stdin pipe, read response from
  stdout pipe; framing correct.
- `TestMCPInitialize`: mock echo server → `initialize` → `initialized` handshake.
- `TestMCPToolList`: mock server returns two tools; `ToolRegistry` exposes both.
- `TestMCPRestartBackoff`: after three crashes, restart intervals are 1s, 2s, 4s.

### `internal/lsp`
- `TestLSPInitialize`: mock LSP server (echo) → `initialize` → `initialized`.
- `TestLSPDiagnosticPublish`: `textDocument/publishDiagnostics` notification →
  `lsp.updated` bus event with correct session payload.

### `internal/resource` (Plan 04 loaders)
- `TestAgentLoad`: parse `~/.opencode/agents/coder.md` frontmatter; assert
  `name`, `model`, `permission` fields.
- `TestCommandLoad`: parse `.opencode/commands/deploy.md`; assert `description` and body.
- `TestRuleLoad`: parse `.opencode/rules/*.md`; rules injected into system prompt.
- `TestSkillLoad`: discover `.opencode/skills/` directory; loader returns metadata list.

---

## Daemon Integration Tests

These tests spin a real `forged` process (or use the `daemon.TestDaemon` helper that
starts it in-process) and exercise actual HTTP/SSE endpoints.

Location: `tests/integration/daemon/`

### Harness

```go
// TestDaemon starts forged in-process with an isolated tmpdir and SQLite.
// It returns a client pointed at the test server.
type TestDaemon struct {
    Client  *sdk.Client   // generated Go client (plan 06)
    Dir     string        // temp directory
    Cleanup func()
}

func NewTestDaemon(t *testing.T, opts ...DaemonOption) *TestDaemon
// DaemonOption: WithConfig(...), WithGitRepo(), WithMockLLM(), WithMockMCP()
```

### Session lifecycle tests
- `TestCreateSession`: `POST /session` → 200 with session JSON; `GET /session` lists it.
- `TestDeleteSession`: create then delete; `GET /session/:id` → 404.
- `TestSessionFork`: fork a session; child has `parentID`; both have independent message histories.
- `TestSessionRevert`: run a prompt that writes a file; revert; file content restored.

### SSE event sequence tests (golden files)
Each test records the SSE event stream for a scenario and diffs against a golden
file in `tests/integration/daemon/testdata/sse_golden/`.

Golden file format (one JSON object per line, type + properties, volatile fields
like `id` and `timestamp` stripped by normalizer):
```
{"type":"server.connected","properties":{}}
{"type":"session.created.1","properties":{"sessionID":"<id>"}}
{"type":"message.updated.1","properties":{"role":"user",...}}
...
```

Scenarios with golden SSE files:
- `sse_create_session` — connect SSE, create session; assert `server.connected` + `session.created.1`.
- `sse_prompt_text_only` — mock LLM returns pure text; assert `message.updated.1` + `part.updated` sequence.
- `sse_prompt_tool_call` — mock LLM returns one tool call; assert `part.updated (pending)` → `part.updated (running)` → `part.updated (completed)`.
- `sse_permission_asked` — mock LLM calls bash tool; permission ruleset requires ask; assert `permission.asked` → reply → `part.updated (completed)`.
- `sse_heartbeat` — no activity for 10s; assert `server.heartbeat` event received.
- `sse_reconnect` — disconnect and reconnect; assert `server.connected` on reconnect (replay via `/sync/replay` is plan 12's territory).

### Prompt integration tests (mock LLM)
- `TestPromptSync`: `POST /session/:id/prompt` blocks; returns `MessageWithParts` JSON.
- `TestPromptAsync`: `POST /session/:id/prompt_async` → 204; events arrive via SSE.
- `TestPromptCancel`: start async prompt; `POST /session/:id/abort`; event stream ends.
- `TestPromptBusy`: two concurrent `POST /session/:id/prompt` calls; second gets 409.

### Permission round-trip test
```
TestPermissionRoundTrip:
  1. Start daemon with mock LLM script: "call bash tool"
  2. Open SSE connection
  3. POST /session/:id/prompt_async
  4. Wait for permission.asked event; extract permissionID
  5. POST /session/:id/permissions/:permissionID {"response":"once"}
  6. Wait for part.updated with status=completed
  7. Wait for message.updated with role=assistant
  PASS
```

### PTY tests
- `TestPTYCreate`: `POST /pty` → PTY info with ID.
- `TestPTYConnect`: WebSocket connect to `/pty/:id/connect`; send resize control
  frame `0x00 + {"cursor":{"rows":24,"cols":80}}`; send input; read output.
  Reference: PTY WS framing spec in plan 00 (`0x00 + JSON({cursor})`).
- `TestPTYAuthBypass`: connect with valid one-time ticket; no Basic Auth header needed.

### Config endpoint tests
- `TestConfigGet`: `GET /config` → config JSON matching schema.
- `TestConfigProviders`: `GET /config/providers` → list of configured providers.
- `TestGlobalHealth`: `GET /global/health` → `{"ok":true}`.

---

## Mock-LLM Agent Loop Tests

The agent engine must be testable without real API calls. A `MockLLMProvider`
accepts a deterministic script of responses.

Location: `internal/agent/mock_llm.go` (test helper).

```go
type MockLLMScript []MockLLMStep

type MockLLMStep struct {
    // Exactly one of:
    TextResponse   string
    ToolCall       MockToolCall
    ErrorResponse  error
}

type MockToolCall struct {
    Name  string
    Input map[string]any
}

// MockLLMProvider replays the script in order.
// It fails the test if the agent makes more LLM calls than scripted.
type MockLLMProvider struct {
    t      *testing.T
    script MockLLMScript
    pos    int
}
```

### Agent loop unit tests (using MockLLM)

- `TestAgentLoopTextOnly`: script = [TextResponse("hello")]; assert one assistant
  message with one text part, no tool calls.
- `TestAgentLoopToolCall`: script = [ToolCall(bash, {cmd:"ls"}), TextResponse("done")];
  assert tool part created, executed, result fed back, final text part exists.
- `TestAgentLoopMultiTurn`: three-step script; assert correct message ordering.
- `TestAgentLoopDoomLoop`: script = [ToolCall(bash,x), ToolCall(bash,x), ToolCall(bash,x)];
  assert `permission.asked` for `doom_loop` after third identical call.
  Reference: `processor.ts:424-448` (DOOM_LOOP_THRESHOLD = 3).
- `TestAgentLoopPermissionReject`: tool call; permission reply = `reject`;
  assert tool part `status=error`, loop continues (or stops based on agent config).
- `TestAgentLoopCompaction`: long conversation hits token limit; assert compaction
  event published and context window reduced.
- `TestAgentLoopRetry`: LLM returns error on first call; script retries up to
  configured max; assert final success.
- `TestAgentLoopOrphanedTool`: tool part with `status=error + interrupted=true`
  marker is not replayed to LLM. Reference: `prompt.ts:84-88`.

### Structured output test
- `TestStructuredOutput`: agent configured with output schema; mock LLM calls
  `StructuredOutput` tool; assert result decoded and returned.

---

## Ecosystem Tests: MCP, LSP, Resources

### MCP tests with a stdio echo server

A tiny Go-based MCP echo server in `tests/testdata/mcp-echo-server/` implements:
- `initialize` / `initialized`
- `tools/list` → `[{name:"echo", description:"echo input"}]`
- `tools/call` `echo` → returns `{content:[{type:"text",text:input}]}`

Test scenarios:
- `TestMCPEchoServerDiscovery`: configure echo server in daemon config; after
  init, `GET /mcp` lists the server as `connected`.
- `TestMCPEchoToolInAgentLoop`: mock LLM calls `echo` tool; result returned
  correctly; `mcp.updated` SSE event published.
- `TestMCPServerRestart`: kill echo server; daemon detects exit; restarts with
  backoff; after restart tool calls resume.
- `TestMCPMultipleServers`: configure two echo servers; both appear in tool list;
  name collisions handled (server-prefixed names).

### LSP tests with gopls

Gated on `which gopls` being available (skip if not).
- `TestLSPGoplsDiagnostics`: write a Go file with a syntax error to the temp dir;
  `textDocument/didOpen`; wait for `lsp.updated` SSE event; assert diagnostics
  contain the error.
- `TestLSPInitializeShutdown`: initialize gopls; send `shutdown`; assert clean exit.

### Resource loader tests (Plan 04)
These are unit tests but require filesystem fixtures:
- `testdata/resources/agents/` with sample agent markdown files.
- `testdata/resources/commands/` with sample command files.
- `testdata/resources/rules/` with sample rule files.

Tests assert that the loader correctly merges project-level and global resources,
that malformed files produce warnings (not panics), and that hot-reload picks up
newly added files.

---

## Client Tests

### Mobile UI tests (Plan 07)

Location: `mobile/app/src/androidTest/`

Framework: Jetpack Compose UI Testing + `composeTestRule`.

Scenarios (run against mock server from plan 06 Kotlin SDK):
- `SessionListTest`: assert session list renders; tap session → detail screen.
- `MessageStreamTest`: mock SSE stream delivers `message.updated` events; assert
  message list updates.
- `ToolCallBubbleTest`: `part.updated` with tool part → tool bubble renders with
  correct tool name and status indicators.
- `PermissionDialogTest`: `permission.asked` event → permission dialog shown;
  tap "Allow Once" → verify permission reply sent.
- `PTYScreenTest`: PTY session created; keyboard input sent; output rendered.
- `OfflineTest`: lose network; reconnect banner shown; SSE reconnect on restore.

Mock server for mobile tests: a lightweight Kotlin `MockWebServer` (OkHttp) that
replays pre-recorded SSE sequences from plan 12 cassette files.

### TUI smoke tests (Plan 08)

Location: `tests/integration/tui/`

The Bubble Tea TUI is tested by driving it via `os/exec` and asserting on its
terminal output using the `vhs` tool or a custom VT100 parser.

- `TestTUIStartup`: launch `forged tui`; assert initial session list renders
  within 2s.
- `TestTUIPromptSubmit`: type a prompt; mock LLM returns text; assert text
  appears in message pane.
- `TestTUIToolCall`: mock LLM calls a tool; assert tool bubble rendered with
  spinner then checkmark.
- `TestTUIPermissionPrompt`: mock LLM triggers permission ask; assert modal dialog;
  send Enter to approve; assert tool completes.
- `TestTUIAttach`: start daemon separately; run `forged tui --attach <url>`;
  assert connects and session list populates.

---

## Fixtures and Harness

### Test fixture directory
```
tests/testdata/
    config/
        valid.toml
        project-override.toml
        missing-provider.toml
    sessions/
        sample-session.json         # MessageWithParts for replay tests
    mcp-echo-server/
        main.go                     # tiny Go MCP echo server
    sse_golden/
        sse_create_session.ndjson
        sse_prompt_text_only.ndjson
        sse_prompt_tool_call.ndjson
        sse_permission_asked.ndjson
    resources/
        agents/coder.md
        commands/deploy.md
        rules/safe.md
```

### Golden file normalizer
Before diffing SSE golden files, volatile fields are stripped:
- `id` fields (ULIDs) → `"<id>"`
- `timestamp` / `time.*` fields → `"<ts>"`
- `sessionID`, `messageID`, `partID` → `"<id>"` (preserve relative references)
- File paths → `"<path>"`

The normalizer is a standalone tool: `cmd/normalize-sse/main.go`.

### `testutil` package
Shared test helpers in `internal/testutil/`:
- `NewTempDir(t) string` — creates temp dir with `t.Cleanup`.
- `NewGitRepo(t, dir string)` — runs `git init` + initial commit.
- `WaitForSSEEvent(t, events <-chan Event, predicate func(Event) bool, timeout time.Duration)`.
- `AssertEventSequence(t, got []Event, want []EventMatcher)` — ordered match
  with optional gaps (`...` wildcard).

---

## CI Wiring

```yaml
# .github/workflows/test.yml
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - go test ./... -count=1 -race -timeout 2m

  integration:
    runs-on: ubuntu-latest
    steps:
      - go test ./tests/integration/... -count=1 -timeout 5m
      # LSP tests: install gopls
      - go install golang.org/x/tools/gopls@latest

  mobile:
    runs-on: ubuntu-latest
    steps:
      - ./gradlew connectedAndroidTest  # or use emulator in CI

  tui:
    runs-on: ubuntu-latest
    steps:
      - go test ./tests/integration/tui/... -count=1 -timeout 3m
      # Requires: apt-get install vhs (or pre-built binary)

  live:
    runs-on: ubuntu-latest
    if: github.event_name == 'workflow_dispatch'
    env:
      FORGE_LIVE_TESTS: "1"
      ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
    steps:
      - go test ./tests/live/... -count=1 -timeout 10m
```

---

## Coverage Goals

| Package | Target line coverage |
|---------|---------------------|
| `internal/config` | 90% |
| `internal/permission` | 90% |
| `internal/agent` (model) | 85% |
| `internal/session` (store) | 85% |
| `internal/transport` | 80% |
| `internal/tool` | 80% |
| `internal/mcp` | 75% |
| `internal/lsp` | 70% |
| `internal/resource` | 80% |
| Daemon integration (behavior coverage) | All plan 09 milestone scenarios passing |

Coverage is not a gate on its own; the gate is: all unit and integration tests
pass, all golden SSE files match, and the plan 12 conformance suite is green.

---

## What "Functionally Correct" Means per Component

| Component (plan) | Correctness criterion |
|-----------------|----------------------|
| Daemon-core (01) | All 113 openapi.json paths respond with correct status codes and JSON shapes; SSE delivers every bus event to all subscribed clients |
| Agent-engine (02) | Given deterministic mock LLM script, message + part state in SQLite matches expected sequence; permission events fire at correct points |
| MCP/LSP (03) | Tools registered from MCP server are callable and return correct results; LSP diagnostics arrive as `lsp.updated` events |
| Resources (04) | All resource types loaded from `.opencode/` directory; hot-reload picks up additions |
| Plugin-host (05) | TS plugin `tool.execute.before` hook fires before tool call; `tool.execute.after` fires after |
| SDK (06) | Generated Go client can make every endpoint call and deserialize responses without type errors |
| Mobile (07) | All Compose UI scenarios pass; permission dialog flow completes end-to-end |
| TUI (08) | Startup, prompt, tool call, permission approval all render correctly in terminal |

---

## Links

- [09-integration](09-integration.md) — component wiring and sequence diagrams
- [11-test-performance](11-test-performance.md) — benchmark strategy
- [12-test-compatibility](12-test-compatibility.md) — wire conformance harness
- [02-agent-engine](02-agent-engine.md) — mock LLM provider interface
- [01-daemon-core](01-daemon-core.md) — TestDaemon harness design
