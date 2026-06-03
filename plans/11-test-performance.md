# Plan 11 — Performance and Load Test Strategy

> Scope: benchmark suites, SLOs, head-to-head methodology vs opencode's TS/Bun
> daemon, profiling workflow, and CI regression gating.
> Key motivation: Go gives us a faster, lower-footprint daemon — this plan
> defines how we measure and prove that claim.

---

## Context

opencode's daemon is TypeScript on Bun. It is single-process, single-user, and
starts with meaningful warm-up overhead (TS JIT, Bun runtime init, Effect runtime
init). Forge replaces it with a Go binary compiled to native code.

The performance claim is not "Go is always faster than Bun" (hot JS is fast). It is:
1. **Startup time** is deterministically faster: no JIT warm-up.
2. **Idle memory footprint** is lower: no V8/Bun heap overhead.
3. **Concurrent session throughput** scales better: goroutine-per-request is
   cheaper than Bun's event loop under concurrent load.
4. **SSE fan-out latency** is lower: direct channel broadcast vs event-loop microtask queuing.
5. **Daemon-side tool-loop overhead** (excluding model latency) is lower.

All claims are verified by the head-to-head methodology defined below. The
comparison is intentionally fair: same workload, same machine, same mock provider.

Reference: opencode daemon entry at `packages/opencode/src/server/server.ts`;
SSE handler at `packages/opencode/src/server/routes/instance/httpapi/handlers/event.ts`;
prompt handler at `packages/opencode/src/session/prompt.ts`.

---

## Metrics and SLOs

> **No baseline has been measured yet — these are aspirational targets, not measurements.**
> The "vs opencode" column is a *hypothesis* to confirm. **W0 of this plan is to measure the real
> opencode daemon** on the same machine + workload and record actual baselines; only then are the
> Forge SLOs and the multipliers below validated or revised. Do not cite any "Nx faster" figure
> until both daemons have been run head-to-head per the methodology in this doc.

| Metric | SLO (Forge, target) | Aspirational ratio vs opencode (UNMEASURED) |
|--------|---------------------|---------------------------------------------|
| Cold start to first `/global/health` 200 | < 50ms | ≥ 5x faster than opencode |
| Cold start to first `/event` SSE connected | < 100ms | ≥ 3x faster than opencode |
| Idle RSS (no sessions) | < 30 MB | ≥ 5x lower than opencode |
| RSS with 10 concurrent sessions | < 80 MB | ≥ 3x lower than opencode |
| Time-to-first SSE event after POST /prompt_async | < 5ms daemon overhead | ≥ 2x lower than opencode |
| SSE fan-out p99 latency (1 publisher, 50 subscribers) | < 1ms | ≥ 2x lower than opencode |
| SSE fan-out p99 latency (1 publisher, 200 subscribers) | < 5ms | measure only |
| POST /session throughput (no agent) | ≥ 2 000 req/s | ≥ 3x opencode |
| GET /session/:id throughput (SQLite read) | ≥ 5 000 req/s | ≥ 3x opencode |
| Tool-loop daemon overhead per iteration (mock LLM, 0 network) | < 1ms | ≥ 2x lower than opencode |
| PTY streaming throughput | ≥ 50 MB/s | ≥ 2x opencode |
| SQLite write throughput (concurrent parts) | ≥ 500 writes/s | measure only |

SLOs are minimum acceptance criteria for the Go daemon in isolation. Head-to-head
goals are best-effort; the tooling records deltas for trend analysis.

---

## Benchmark Suites

### Suite 1: Startup benchmarks

**Method**: shell `time` + Go `testing.B`.
- `BenchmarkStartupHealthCheck`: fork `forged` as child process; time from
  `cmd.Start()` to first successful `GET /global/health`; 20 iterations.
- `BenchmarkStartupSSEConnect`: fork daemon; time to first SSE `server.connected`
  event on `GET /event`; 20 iterations.
- opencode equivalent: same shell timing against `opencode serve`.

**Tool**: custom Go benchmark in `bench/startup_test.go`.

```go
func BenchmarkStartupHealthCheck(b *testing.B) {
    for b.N > 0; b.N-- {
        cmd := exec.Command("./forged", "--dir", tmpDir)
        start := time.Now()
        cmd.Start()
        waitForHTTP(b, "http://127.0.0.1:"+port+"/global/health", 5*time.Second)
        b.ReportMetric(float64(time.Since(start).Milliseconds()), "ms/startup")
        cmd.Process.Kill()
    }
}
```

### Suite 2: Memory footprint benchmarks

**Method**: measure RSS via `/proc/PID/status` (Linux) or `ps -o rss` (macOS)
at steady state (after init completes, before any requests).

- `BenchmarkIdleRSS`: start daemon, wait 2s, sample RSS.
- `BenchmarkRSSWith10Sessions`: create 10 sessions via API, wait 1s, sample RSS.
- `BenchmarkRSSWith50Sessions`: same with 50 sessions.

**Tool**: Go benchmark + `internal/bench/rss_linux.go` / `rss_darwin.go` helpers.

### Suite 3: SSE fan-out latency benchmark

**Method**: one goroutine publishes events at a fixed rate; N goroutines each
hold an open `GET /event` SSE connection and measure latency from publish to
receive (monotonic clock delta embedded in event properties).

```go
func BenchmarkSSEFanout50(b *testing.B) {
    benchSSEFanout(b, 50)
}
func BenchmarkSSEFanout200(b *testing.B) {
    benchSSEFanout(b, 200)
}
```

**Tool**: custom benchmark + `bench/sse_fanout_test.go`.

Latency injected: publisher adds `publishedAt: unixNanoMono()` to event properties;
each subscriber records `receivedAt` and reports the delta.

### Suite 4: HTTP endpoint throughput

**Method**: `vegeta` (HTTP load testing tool) or `k6`.
Target endpoints:
- `POST /session` (creates a session; SQLite write)
- `GET /session` (list; SQLite read)
- `GET /session/:id` (fetch; SQLite read)
- `GET /global/health` (no I/O; pure router overhead)

Attack profile:
```
vegeta attack -duration=30s -rate=500 -targets=targets.txt | vegeta report
```

Ramp: 100, 250, 500, 1 000, 2 000 req/s. Report p50, p95, p99, max latency and
error rate at each level. Stop ramping when error rate exceeds 1%.

**Tool**: `vegeta` (Go binary, in PATH in CI).

### Suite 5: Daemon-side tool-loop overhead

**Method**: Use the MockLLMProvider from plan 10 that returns instantly (0 network
latency). Run N tool-call rounds through the full agent engine stack and measure
wall time excluding mock LLM response time.

- `BenchmarkToolLoopOverhead1Step`: 1 tool call per prompt; 1 000 iterations.
- `BenchmarkToolLoopOverhead10Steps`: 10 tool calls per prompt; 100 iterations.

Measures: time spent in `AgentEngine.Prompt()` minus the time the mock LLM spent
"thinking" (measured separately inside the mock).

**Tool**: `go test -bench=BenchmarkToolLoop` in `internal/agent/bench_test.go`.

### Suite 6: PTY streaming throughput

**Method**: Create a PTY session; connect via WebSocket; send input that triggers
a command producing large output (`cat /dev/urandom | head -c 10MB`); measure
bytes/second received by the WebSocket client.

- `BenchmarkPTYThroughput`: target ≥ 50 MB/s.

**Tool**: custom Go benchmark; WebSocket client using `gorilla/websocket`.

### Suite 7: SQLite concurrent write throughput

**Method**: N goroutines each call `SessionStore.UpdatePartDelta()` at maximum
rate for 10 seconds. Measure total writes/second and p99 write latency.

- `BenchmarkSQLiteConcurrentPartWrites_10g`: 10 goroutines.
- `BenchmarkSQLiteConcurrentPartWrites_50g`: 50 goroutines.

**Tool**: `go test -bench=BenchmarkSQLite` in `internal/session/bench_test.go`.

### Suite 8: MCP/LSP subprocess overhead

**Method**: Start 5 MCP echo servers; run 1 000 tool calls through each concurrently;
measure total time and per-call latency.

- `BenchmarkMCPOverhead5Servers`: 5 servers × 200 calls each.
- `BenchmarkLSPDiagnosticLatency`: write file; time from `didChange` notification to
  `lsp.updated` SSE event.

**Tool**: `go test -bench=BenchmarkMCP` in `internal/mcp/bench_test.go`.

---

## Head-to-Head vs opencode Methodology

### Setup
- Same physical machine (or same CI runner class) for both runs.
- Same Git commit of opencode (pin to a specific tag, e.g. `v0.3.x`).
- `forged` built with `CGO_ENABLED=0 GOARCH=amd64 GOOS=linux` (or native arch).
- opencode daemon started with `opencode serve --port 4096`.
- Forge daemon started with `forged --port 4097`.
- Both use the same SQLite-on-tmpfs scratch directory.
- **Mock LLM**: both use a local HTTP echo server that returns a fixed JSON
  response in < 1ms. For opencode: set `OPENAI_BASE_URL=http://localhost:9999/v1`;
  for Forge: use the built-in `MockLLMProvider`.

### Workloads

Three standard workloads, run against both daemons:

**W1 — Startup**: 20 cold starts each; report mean and stddev of time-to-first-health.

**W2 — Single-session throughput**: 1 000 `POST /session/:id/prompt_async` requests
in sequence; 1 SSE subscriber per session; measure end-to-end prompt latency
(request submit → final `message.updated` SSE event).

**W3 — Concurrent sessions**: 10 concurrent sessions each submitting 100 prompts;
measure total wall time and per-session latency.

### Report format

**ILLUSTRATIVE TEMPLATE ONLY — the cells below are placeholders, NOT measurements.** No run
has been performed; the runner (below) populates `forge` and `opencode` columns from real data,
computes `ratio`, and fills `SLO met?` against the targets table above.
```
metric                     forge      opencode   ratio   SLO met?
startup_p50_ms             <fill>     <fill>     <calc>  <calc>
startup_p99_ms             <fill>     <fill>     <calc>  <calc>
idle_rss_mb                <fill>     <fill>     <calc>  <calc>
rss_10_sessions_mb         <fill>     <fill>     <calc>  <calc>
sse_fanout_50_p99_us       <fill>     <fill>     <calc>  <calc>
post_session_rps           <fill>     <fill>     <calc>  <calc>
prompt_e2e_p50_ms          <fill>     <fill>     <calc>  <calc>
tool_loop_overhead_us      <fill>     <fill>     <calc>  <calc>
```

### Automation

The head-to-head runner is `bench/head_to_head_test.go`. It:
1. Starts both daemons (forked processes).
2. Runs all three workloads against both.
3. Writes a JSON report to `bench/results/YYYY-MM-DD-HH.json`.
4. Optionally posts a summary comment on PRs via GitHub Actions.

---

## Profiling Workflow

When a benchmark shows unexpected regression, the profiling workflow is:

### CPU profile
```bash
go test -bench=BenchmarkSSEFanout50 -cpuprofile cpu.prof ./bench/
go tool pprof -http=:6060 cpu.prof
```

### Memory profile
```bash
go test -bench=BenchmarkRSSWith10Sessions -memprofile mem.prof ./bench/
go tool pprof -http=:6061 mem.prof
```

### Goroutine leak check
```bash
# Use goleak in tests
import "go.uber.org/goleak"
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

### Continuous profiling (production)
`forged` exposes `GET /debug/pprof/*` behind the auth middleware when
`FORGE_PPROF_ENABLED=1`. This enables on-demand profiling of live instances.

### Trace for SSE latency
```bash
go test -bench=BenchmarkSSEFanout200 -trace trace.out ./bench/
go tool trace trace.out
```
Look for: goroutine scheduling delays, channel contention on the bus broadcast.

---

## Regression Gating in CI

A benchmark regression gate runs on every PR that touches:
- `internal/transport/` (SSE bus, HTTP handler)
- `internal/session/` (SQLite store)
- `internal/agent/` (tool loop)

Gate implementation:
```yaml
# .github/workflows/bench.yml
jobs:
  bench:
    runs-on: ubuntu-latest
    steps:
      - name: Run benchmarks
        run: go test -bench=. -benchmem -count=5 ./bench/... > bench.txt
      - name: Compare with baseline
        uses: benchmark-action/github-action-benchmark@v1
        with:
          tool: 'go'
          output-file-path: bench.txt
          alert-threshold: '120%'   # fail if 20% slower than baseline
          fail-on-alert: true
          comment-on-alert: true
```

Baseline is stored in `gh-pages` branch by `benchmark-action`. A 20% regression
in any benchmark fails the PR. Manual override available for intentional trade-offs
(e.g. correctness fix that costs 5% throughput).

---

## Risks and Fair-Comparison Caveats

| Risk | Notes |
|------|-------|
| Mock LLM not equivalent | opencode uses AI SDK which adds middleware overhead; Forge's mock bypasses this. Measure only daemon-side overhead explicitly; don't conflate with LLM latency. |
| SQLite WAL mode differences | opencode may use different SQLite pragmas. Align pragmas before comparing. Document configuration in bench runner. |
| GC pauses in Go | Go GC can cause latency spikes at high allocation rates. Profile with `GOGC=off` and `GOGC=100` to characterize. |
| Bun JIT warm-up | First N requests to opencode are slower due to JIT. Warm up both daemons with 100 un-measured requests before recording results. |
| Resource loader cost | Forge loads agents/rules/skills on startup; opencode may be lazy. Measure with and without resource loading enabled. |
| CGO vs pure Go SQLite | Pure Go SQLite (`modernc.org/sqlite`) is slower than CGO SQLite. Decide per plan 01 and document the choice here. |
| Machine variance | Run benchmarks 5 times each; report mean ± stddev; discard outliers > 2σ. |

---

## Review pass (2026-06-03) — disciplined; one prerequisite to pin

This plan correctly honors the masterplan non-negotiable (every number is an UNMEASURED target;
W0 = measure opencode first). The caveats are thorough. Minimal additions:

- **Status: W0 not done.** No baseline has been recorded and no benchmark harness is built yet, so
  every ratio in the table remains a hypothesis. Until W0 runs, this plan contributes zero validated
  claims — keep that explicit so no "Nx faster" figure leaks into docs/marketing.
- **The unsolved prerequisite is a deterministic provider for *opencode*, not Forge.** The "same
  mock provider" fairness condition requires opencode to be driven by a scripted, zero-network LLM.
  Forge owns its mock (`internal/engine/enginetest`), but opencode does not expose one — so W0 must
  first stand up a **local OpenAI-compatible mock HTTP endpoint** that *both* daemons point at (same
  scripted SSE). Without it the head-to-head conflates daemon overhead with provider middleware.
  This is the gating task; add it as W0.a before any measurement.
- **CGO-vs-pure-Go SQLite is already decided** (caveat says "decide per plan 01"): the repo uses
  **pure-Go `modernc.org/sqlite`** per the masterplan non-negotiables. Record it as decided and
  measure within that constraint — a CGO comparison is out of scope, not an open choice.
- **Resource-loader cost** (caveat): plan 04 loaders are eager and now built; measure startup
  **with** them enabled as the default, since that is the shipping behavior.

## W0 update (2026-06-03) — first baseline recorded

W0 is **partially done**: a reproducible head-to-head harness now lives in [`bench/`](../bench/) and a
first baseline has been recorded against the **real opencode daemon** (`opencode` 1.15.12) and `forged`
on an Apple M1 Mac mini. See [`bench/results/`](../bench/results/) for the dated, machine-described
numbers and [`bench/README.md`](../bench/README.md) for methodology and caveats.

- **Measured (this pass):** cold start, idle RSS, SSE *connection* fan-out (dial → `server.connected`,
  N subscribers + RSS-with-subs), and request throughput (`GET /global/health`, `GET /session`). On the
  M1 baseline, forged measured materially faster/leaner on every metric — but those ratios are valid
  only for that host + those versions; **do not generalize them**. The harness re-derives ratios from
  real data on each run.
- **Still open (correctly deferred, not fabricated):**
  - W0.a's **deterministic local mock LLM** (scripted OpenAI-compatible endpoint both daemons share) is
    still the gating prerequisite for the prompt/tool-loop workloads (W2/W3, Suites 5–8). Until it
    exists, those rows stay unmeasured.
  - **Per-event** SSE publish→receive fan-out (Suite 3) needs a symmetric trigger both daemons emit on
    demand; opencode publishes `session.updated` on create, Forge's create path does not yet publish.
    The current harness measures connection fan-out (symmetric) in the interim.
- The pure-Go-SQLite constraint and resource-loader-enabled startup from the review pass above are both
  honored by the harness (forged is measured as it ships).

## Links

- [09-integration](09-integration.md) — component wiring; SSE bus architecture
- [10-test-functional](10-test-functional.md) — MockLLMProvider definition
- [12-test-compatibility](12-test-compatibility.md) — conformance correctness gate
- [01-daemon-core](01-daemon-core.md) — SQLite, SSE bus, transport details
- [02-agent-engine](02-agent-engine.md) — tool loop implementation
