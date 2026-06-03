//go:build bench

package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestBaseline runs the W0 head-to-head baseline against the two daemons named
// in the environment and writes a dated JSON + Markdown report under
// bench/results/. It is a test only so `go test -tags bench` can drive it; it
// makes no assertions about ratios — W0 records reality, it does not gate on it.
//
// Required env (set by bench/run_baseline.sh):
//
//	BENCH_FORGE_BIN, BENCH_FORGE_PORT
//	BENCH_OPENCODE_BIN, BENCH_OPENCODE_PORT
//	BENCH_USER, BENCH_PASS          (Basic auth applied to both)
//	BENCH_DIR                       (x-opencode-directory for routed endpoints)
//	BENCH_RESULTS_DIR               (output directory)
//	BENCH_OPENCODE_HOME             (HOME for the opencode child; isolates its DB)
//
// Optional tuning env (defaults shown):
//
//	BENCH_COLDSTART_ITERS=10
//	BENCH_SUBS=50
//	BENCH_TP_CONCURRENCY=16
//	BENCH_TP_SECONDS=5
func TestBaseline(t *testing.T) {
	forgeBin := mustEnv(t, "BENCH_FORGE_BIN")
	forgePort := mustEnvInt(t, "BENCH_FORGE_PORT")
	ocBin := mustEnv(t, "BENCH_OPENCODE_BIN")
	ocPort := mustEnvInt(t, "BENCH_OPENCODE_PORT")
	user := os.Getenv("BENCH_USER")
	pass := os.Getenv("BENCH_PASS")
	dir := os.Getenv("BENCH_DIR")
	resultsDir := mustEnv(t, "BENCH_RESULTS_DIR")
	ocHome := os.Getenv("BENCH_OPENCODE_HOME")

	coldIters := envInt("BENCH_COLDSTART_ITERS", 10)
	subs := envInt("BENCH_SUBS", 50)
	tpConc := envInt("BENCH_TP_CONCURRENCY", 16)
	tpSecs := envInt("BENCH_TP_SECONDS", 5)

	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatalf("mkdir results: %v", err)
	}

	// Both daemons bind 127.0.0.1 and speak the same wire surface. opencode
	// needs an isolated HOME so its SQLite DB does not bleed across runs; the
	// child env inherits the parent's PATH etc.
	forgeEnv := append(os.Environ(),
		"OPENCODE_SERVER_USERNAME="+user,
		"OPENCODE_SERVER_PASSWORD="+pass,
	)
	ocEnv := append(os.Environ(),
		"OPENCODE_SERVER_USERNAME="+user,
		"OPENCODE_SERVER_PASSWORD="+pass,
	)
	if ocHome != "" {
		ocEnv = append(ocEnv, "HOME="+ocHome)
	}

	forge := Target{
		Name: "forge",
		Bin:  forgeBin,
		Args: []string{"--port", strconv.Itoa(forgePort), "--host", "127.0.0.1"},
		Env:  forgeEnv,
		Port: forgePort, User: user, Pass: pass, Dir: dir,
	}
	opencode := Target{
		Name: "opencode",
		Bin:  ocBin,
		Args: []string{"serve", "--port", strconv.Itoa(ocPort), "--hostname", "127.0.0.1"},
		Env:  ocEnv,
		Port: ocPort, User: user, Pass: pass, Dir: dir,
	}

	cfg := ReportConfig{
		ColdStartIters:    coldIters,
		SubCount:          subs,
		ThroughputConc:    tpConc,
		ThroughputSeconds: tpSecs,
		OpencodeVersion:   captureVersion(opencode),
		ForgeVersion:      captureForgeVersion(forge),
	}

	// Bound the whole run with an internal deadline so a single stuck daemon
	// iteration fails fast against ctx.Done() rather than parking until the
	// `go test -timeout` kills the process and loses the partial report. The
	// cap is generous (covers warm + cold-start iters + the live suite for two
	// daemons) but finite, so every ctx.Done() backstop in the harness actually
	// fires. Overridable via BENCH_RUN_TIMEOUT_SECONDS.
	runTimeout := time.Duration(envInt("BENCH_RUN_TIMEOUT_SECONDS", 600)) * time.Second
	ctx, cancelRun := context.WithTimeout(context.Background(), runTimeout)
	defer cancelRun()
	// Daemon stdout/stderr logs are run artifacts, not results: write them to a
	// scratch dir (BENCH_LOG_DIR) so bench/results/ holds only the JSON + MD.
	logDir := os.Getenv("BENCH_LOG_DIR")
	if logDir == "" {
		logDir = t.TempDir()
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logdir: %v", err)
	}

	var results []MetricResult
	for _, tgt := range []Target{forge, opencode} {
		// One-time on-disk migration warm-up (opencode migrates its SQLite DB on
		// first boot against a fresh HOME). Run once, un-timed, so the cold-start
		// numbers reflect steady-state startup, not a one-off migration.
		warm(ctx, t, tgt, logDir)

		mr := MetricResult{Target: tgt.Name, SubCount: subs}

		cs, err := MeasureColdStart(ctx, tgt, coldIters, logDir)
		if err != nil {
			t.Fatalf("%s cold start: %v", tgt.Name, err)
		}
		mr.ColdStart = cs
		t.Logf("%s cold-start: p50=%.1fms p99=%.1fms (n=%d)", tgt.Name, cs.P50Ms, cs.P99Ms, cs.N)

		// Idle RSS, SSE fan-out, RSS-with-subs and throughput are all measured on
		// a SINGLE live process so the idle->with-subs delta is apples-to-apples
		// (separate spawns drift with GC timing). measureLive owns that process.
		live := measureLive(ctx, t, tgt, subs, tpConc, time.Duration(tpSecs)*time.Second, logDir)
		mr.IdleRSSKB = live.idleRSS
		mr.RSSWithSubsKB = live.rssWithSubs
		mr.SubConnected = live.subConnected
		mr.SSEConnect = live.sseConnect
		mr.HealthThroughputRPS = live.healthRPS
		mr.SessionListThroughputRPS = live.sessionRPS
		if live.subConnected < subs {
			mr.Notes = append(mr.Notes, fmt.Sprintf(
				"SSE fan-out reached %d/%d subscribers; rss_with_subs_kb and sse_connect_ms reflect %d live connections, not %d",
				live.subConnected, subs, live.subConnected, subs))
		}
		t.Logf("%s idle RSS=%dKB; sse-connect p50=%.2fms p99=%.2fms; rss+%d/%dsubs=%dKB; health=%.0frps sessionList=%.0frps",
			tgt.Name, live.idleRSS, live.sseConnect.P50Ms, live.sseConnect.P99Ms, live.subConnected, subs, live.rssWithSubs, live.healthRPS, live.sessionRPS)

		results = append(results, mr)
	}

	report := Report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Plan:        "11-test-performance W0 baseline",
		Machine:     collectMachineInfo(),
		Config:      cfg,
		Targets:     results,
		Disclaimer: "Measured head-to-head on the machine named above. " +
			"Numbers are real for this host only; do not generalize or cite a " +
			"speed multiplier without re-running on the target hardware.",
	}

	stamp := time.Now().Format("2006-01-02-1504")
	jsonPath := fmt.Sprintf("%s/%s-baseline.json", resultsDir, stamp)
	b, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(jsonPath, b, 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	mdPath := fmt.Sprintf("%s/%s-baseline.md", resultsDir, stamp)
	if err := os.WriteFile(mdPath, []byte(renderMarkdown(report)), 0o644); err != nil {
		t.Fatalf("write md: %v", err)
	}
	t.Logf("wrote %s and %s", jsonPath, mdPath)
}

// liveMetrics bundles every metric measured on the single live daemon process.
type liveMetrics struct {
	idleRSS      int
	rssWithSubs  int
	subConnected int
	sseConnect   Sample
	healthRPS    float64
	sessionRPS   float64
}

// measureLive spawns one daemon instance and runs idle RSS, the SSE fan-out,
// RSS-with-subs and throughput measurements against it. Using one process keeps
// the idle->with-subs RSS delta apples-to-apples and avoids redundant restarts.
func measureLive(ctx context.Context, t *testing.T, tgt Target, subs, tpConc int, tpDur time.Duration, logDir string) liveMetrics {
	cmd, err := spawn(tgt, logDir+"/"+tgt.Name+"-live.log")
	if err != nil {
		t.Fatalf("%s spawn: %v", tgt.Name, err)
	}
	defer killTree(cmd)
	if _, err := waitHealthy(ctx, tgt, 30*time.Second); err != nil {
		t.Fatalf("%s healthy: %v", tgt.Name, err)
	}

	// Idle RSS: let the runtime settle, then sample before any subscribers or
	// load touch the process.
	select {
	case <-ctx.Done():
		t.Fatalf("%s idle settle: %v", tgt.Name, ctx.Err())
	case <-time.After(2 * time.Second):
	}
	idleRSS, err := rssKB(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("%s idle rss: %v", tgt.Name, err)
	}

	// SSE fan-out next: it opens long-lived connections and is sensitive to
	// ephemeral-port pressure, so run it before the throughput load churns
	// through short-lived sockets.
	fanout, err := MeasureSSEFanout(ctx, tgt, subs, cmd.Process.Pid)
	if err != nil {
		t.Fatalf("%s sse fanout: %v", tgt.Name, err)
	}
	if fanout.Connected < fanout.Requested {
		t.Logf("%s SSE fan-out shortfall: %d/%d subscribers connected; RSS+subs and "+
			"sse-connect reflect the connected subset, not the full N",
			tgt.Name, fanout.Connected, fanout.Requested)
	}

	tpHealth, err := MeasureThroughput(ctx, tgt, "/global/health", tpConc, tpDur)
	if err != nil {
		t.Fatalf("%s health throughput: %v", tgt.Name, err)
	}
	tpSession, err := MeasureThroughput(ctx, tgt, "/session", tpConc, tpDur)
	if err != nil {
		t.Fatalf("%s session throughput: %v", tgt.Name, err)
	}
	return liveMetrics{
		idleRSS:      idleRSS,
		rssWithSubs:  fanout.RSSKB,
		subConnected: fanout.Connected,
		sseConnect:   fanout.Connect,
		healthRPS:    tpHealth,
		sessionRPS:   tpSession,
	}
}

// warm runs the daemon once (un-timed) so any one-time on-disk migration
// completes before the cold-start metric is measured.
func warm(ctx context.Context, t *testing.T, tgt Target, logDir string) {
	cmd, err := spawn(tgt, logDir+"/"+tgt.Name+"-warm.log")
	if err != nil {
		t.Fatalf("%s warm spawn: %v", tgt.Name, err)
	}
	defer killTree(cmd)
	if _, err := waitHealthy(ctx, tgt, 60*time.Second); err != nil {
		t.Fatalf("%s warm healthy: %v", tgt.Name, err)
	}
}

func renderMarkdown(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Forge performance baseline (plan 11, W0)\n\n")
	fmt.Fprintf(&b, "> %s\n\n", r.Disclaimer)
	fmt.Fprintf(&b, "- Generated: `%s`\n", r.GeneratedAt)
	fmt.Fprintf(&b, "- Machine: `%s` — %s %s, %d CPU, %s\n",
		r.Machine.Hostname, r.Machine.OS, r.Machine.Arch, r.Machine.NumCPU, r.Machine.Model)
	fmt.Fprintf(&b, "- Go: `%s`\n", r.Machine.GoVer)
	fmt.Fprintf(&b, "- forge version: `%s`; opencode version: `%s`\n",
		r.Config.ForgeVersion, r.Config.OpencodeVersion)
	fmt.Fprintf(&b, "- Config: cold-start iters=%d, SSE subscribers=%d, throughput=%d workers × %ds\n\n",
		r.Config.ColdStartIters, r.Config.SubCount, r.Config.ThroughputConc, r.Config.ThroughputSeconds)

	// Index targets by name for a head-to-head table.
	byName := map[string]MetricResult{}
	for _, m := range r.Targets {
		byName[m.Target] = m
	}
	f, hasF := byName["forge"]
	o, hasO := byName["opencode"]

	fmt.Fprintf(&b, "| Metric | forge | opencode | ratio (opencode/forge) |\n")
	fmt.Fprintf(&b, "|--------|-------|----------|------------------------|\n")
	row := func(label string, fv, ov float64, unit string, lowerIsBetter bool) {
		fstr, ostr := "n/a", "n/a"
		if hasF {
			fstr = fmt.Sprintf("%.2f%s", fv, unit)
		}
		if hasO {
			ostr = fmt.Sprintf("%.2f%s", ov, unit)
		}
		ratio := "n/a"
		if hasF && hasO && fv != 0 {
			if lowerIsBetter {
				ratio = fmt.Sprintf("%.2fx", ov/fv)
			} else {
				ratio = fmt.Sprintf("%.2fx", fv/ov)
			}
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", label, fstr, ostr, ratio)
	}
	// Label the RSS-with-subs row with the ACTUAL connected count so a shortfall
	// is never mislabeled as the full requested N.
	subsLabel := func(m MetricResult, has bool) string {
		if !has || m.SubConnected == 0 {
			return strconv.Itoa(r.Config.SubCount)
		}
		if m.SubConnected != r.Config.SubCount {
			return fmt.Sprintf("%d of %d", m.SubConnected, r.Config.SubCount)
		}
		return strconv.Itoa(m.SubConnected)
	}
	rssSubsLabel := subsLabel(f, hasF)
	if hasO {
		if ol := subsLabel(o, hasO); ol != rssSubsLabel {
			rssSubsLabel = fmt.Sprintf("forge %s / opencode %s", rssSubsLabel, ol)
		}
	}

	row("Cold start p50 (ms)", f.ColdStart.P50Ms, o.ColdStart.P50Ms, "", true)
	row("Cold start p99 (ms)", f.ColdStart.P99Ms, o.ColdStart.P99Ms, "", true)
	row("Idle RSS (MB)", kbToMB(f.IdleRSSKB), kbToMB(o.IdleRSSKB), "", true)
	row(fmt.Sprintf("RSS w/ %s SSE subs (MB)", rssSubsLabel), kbToMB(f.RSSWithSubsKB), kbToMB(o.RSSWithSubsKB), "", true)
	row("SSE connect p50 (ms)", f.SSEConnect.P50Ms, o.SSEConnect.P50Ms, "", true)
	row("SSE connect p99 (ms)", f.SSEConnect.P99Ms, o.SSEConnect.P99Ms, "", true)
	row("GET /global/health (req/s)", f.HealthThroughputRPS, o.HealthThroughputRPS, "", false)
	row("GET /session (req/s)", f.SessionListThroughputRPS, o.SessionListThroughputRPS, "", false)

	fmt.Fprintf(&b, "\nRatios are derived, not asserted: a value > 1x means forge measured better on this host. ")
	fmt.Fprintf(&b, "They are valid only for this machine and these versions.\n")
	fmt.Fprintf(&b, "\nPercentiles (p50/p99) are linear-interpolated between ranks; with small n a p99 is an ")
	fmt.Fprintf(&b, "interpolated tail estimate, not necessarily the max sample. ")
	fmt.Fprintf(&b, "Throughput is successful 200s within the window divided by the true measured elapsed.\n")

	// Surface any per-target notes (e.g. SSE fan-out shortfalls) so a caveat
	// recorded in the JSON is also visible in the human-readable report.
	for _, m := range r.Targets {
		for _, n := range m.Notes {
			fmt.Fprintf(&b, "\n- note (%s): %s\n", m.Target, n)
		}
	}
	return b.String()
}

func kbToMB(kb int) float64 { return float64(kb) / 1024.0 }

func captureVersion(t Target) string {
	out, err := execOutput(t.Bin, "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func captureForgeVersion(t Target) string { return captureVersion(t) }

func mustEnv(t *testing.T, k string) string {
	v := os.Getenv(k)
	if v == "" {
		t.Fatalf("env %s is required", k)
	}
	return v
}

func mustEnvInt(t *testing.T, k string) int {
	v := mustEnv(t, k)
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("env %s=%q is not an int: %v", k, v, err)
	}
	return n
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
