# Forge performance baseline harness (plan 11, W0)

This directory implements **W0 of [plan 11](../plans/11-test-performance.md)**: measure the **real
opencode daemon** head-to-head against `forged` on the same machine, and record the actual numbers.

> **No fabricated numbers.** Every figure under [`results/`](results/) is an *actual measurement* on
> the machine named in that file. Plan 11's SLO table is a set of *unmeasured targets*; this harness
> is what turns a target into a measurement. Do not cite an "Nx faster" multiplier without re-running
> on the target hardware — the ratios in a result file are valid only for that host and those daemon
> versions.

## What it measures (the four W0 metrics)

| Metric | How |
|--------|-----|
| **Cold start** | Fork the daemon N times; time `cmd.Start()` → first `GET /global/health` 200. p50/p99 over N. |
| **Idle RSS** | Steady-state resident set size (process tree, via `ps`) after a 2s settle, before any load. |
| **SSE connection fan-out** | Open N concurrent `GET /event` subscribers; per-subscriber dial → `server.connected` latency (p50/p99), plus RSS while all N are live. |
| **Request throughput** | `GET /global/health` (pure router) and `GET /session` (SQLite read) req/s under a fixed worker pool. |

The harness is **daemon-agnostic**: forged and opencode expose the same wire-compatible surface
(`/global/health`, `/event` SSE emitting `server.connected`, Basic auth, `x-opencode-directory`
routing — see `cmd/forged/main.go` and opencode `packages/opencode/src/server/routes/instance/httpapi/handlers/event.ts`),
so the identical client code drives both for a fair comparison.

## Running it

```sh
bench/run_baseline.sh
```

The runner builds `forged` from `./cmd/forged`, locates `opencode` on `PATH`, gives opencode an
isolated `$HOME` (so its SQLite DB does not bleed across runs), pre-warms each daemon once (untimed,
to absorb opencode's one-time DB migration), then runs the harness against both and writes a dated
`results/YYYY-MM-DD-HHMM-baseline.{json,md}`.

Tunables (env, defaults shown):

```
BENCH_COLDSTART_ITERS=10   cold-start iterations per daemon
BENCH_SUBS=50              concurrent SSE subscribers for fan-out + RSS
BENCH_TP_CONCURRENCY=16    throughput load workers
BENCH_TP_SECONDS=5         throughput window per endpoint
FORGE_PORT=4097 / OPENCODE_PORT=4096
```

The live harness is behind the **`bench` build tag**, so it is excluded from `go build ./...`,
`go test ./...`, and CI (forking real daemons is slow). Invoke it directly with the tag if you want
to bypass the runner script:

```sh
go test -tags bench -run TestBaseline -count=1 -v ./bench/   # needs the BENCH_* env vars set
```

## Methodology notes / caveats

- **Cold start is honest steady-state startup.** Each iteration is a fresh process. opencode performs
  a one-time SQLite migration on first boot against a fresh `$HOME`; the runner does an untimed warm
  pass first so that migration does not pollute the cold-start number.
- **RSS is the whole process tree.** opencode may fork helpers; `ps`-walking the tree counts them so
  the comparison is fair.
- **idle RSS and RSS-with-subs are sampled on the *same* live process.** For Go, idle RSS can read
  slightly *higher* than RSS-with-N-subscribers: the post-startup allocation peak is reclaimed by the
  GC during the settle window, and 50 idle SSE connections add negligible heap. This is expected, not
  a measurement error.
- **Throughput uses a single window with keep-alive.** The throughput client bounds its connection
  pool (`MaxConnsPerHost`) so it reuses sockets instead of exhausting ephemeral ports. A 5s window is
  enough to rank the daemons but is noisier than a sustained `vegeta` ramp; absolute req/s vary
  run-to-run while the ranking is stable. Only 200s that complete **on or before** the deadline are
  counted, and the rate is divided by the **true measured elapsed** (start → last counted completion),
  not the nominal window, so a late-finishing request cannot bias req/s upward.
- **Percentiles are linear-interpolated.** p50/p99 interpolate between the two nearest ranks rather
  than snapping to the nearest sample. With small n (the fan-out runs ~50 subscribers) nearest-rank
  would force p99 onto the single max observation; interpolation keeps p99 a real tail estimate.
  Every Sample also records `n`, so the sample size behind a percentile is always visible.
- **SSE fan-out records the actual connected count.** If fewer than the requested N subscribers reach
  `server.connected` within the deadline, the result carries `sub_connected` (and a `notes` entry)
  with the real count; the RSS-with-subs and connect-latency figures describe exactly that many live
  connections, never a relabeled full-N fan-out.
- **Pure-Go SQLite is fixed.** Per the masterplan non-negotiables and plan 11's review pass, Forge
  uses `modernc.org/sqlite`; a CGO comparison is out of scope. All SQLite numbers are within that
  constraint.

## Deferred to a later W0 pass (not measured here)

These are real gaps, called out so no one mistakes silence for a measured result:

- **Per-event SSE publish→receive fan-out latency.** Plan 11 Suite 3 wants one publisher fanning an
  event to N subscribers and measuring publish→receive deltas. A *fair* cross-daemon version needs a
  symmetric event both daemons emit on demand. opencode publishes `session.updated` on `POST /session`
  (`packages/opencode/src/session/session.ts:562`); Forge's create path does not yet publish to the
  instance bus (`internal/server/session_handlers.go`). Until that is symmetric, this harness measures
  **connection** fan-out (dial → `server.connected`), which is fully symmetric, instead of per-event
  fan-out. Add per-event fan-out once both daemons publish the same trigger.
- **Tool-loop / prompt e2e, PTY throughput, SQLite write throughput, MCP/LSP overhead** (plan 11
  Suites 5–8 and workloads W2/W3): these need the deterministic local mock LLM endpoint that plan 11's
  review pass flags as the gating prerequisite (a scripted OpenAI-compatible server both daemons point
  at). That mock is not built yet, so these are intentionally out of this W0 slice.
