//go:build bench

// This file (compiled only under the `bench` build tag) is the live measurement
// harness for plan 11's W0 baseline. The package doc lives in doc.go.
//
// It forks a real daemon (opcoded or opencode) as a child process and measures
// four baseline metrics on the host machine:
//
//   - cold start: time from cmd.Start() to first GET /global/health 200
//   - idle RSS: resident set size after the daemon reaches steady state
//   - SSE connection fan-out: time for N concurrent GET /event subscribers to
//     each receive the server.connected event (p50/p99)
//   - HTTP throughput: requests/sec a single endpoint sustains over a window
//
// The harness is daemon-agnostic: both opcoded and opencode expose the same
// wire-compatible surface (GET /global/health, GET /event SSE with
// server.connected, Basic auth, x-opencode-directory routing), so the same code
// drives both for a fair head-to-head comparison.

package bench

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Target describes a daemon to benchmark.
type Target struct {
	// Name is a short label used in reports (e.g. "opcode42", "opencode").
	Name string
	// Bin is the executable to fork.
	Bin string
	// Args are the arguments passed to Bin. The harness expects the daemon to
	// bind 127.0.0.1:Port.
	Args []string
	// Env is the child process environment (defaults to the parent's when nil).
	Env []string
	// Port is the TCP port the daemon binds.
	Port int
	// User/Pass are the Basic-auth credentials sent on every request. Empty
	// disables auth.
	User string
	Pass string
	// Dir is the project directory sent via x-opencode-directory for
	// directory-routed endpoints (/event, /session). When empty the daemon's
	// startup cwd is used.
	Dir string
}

// baseURL is the http origin for the target.
func (t Target) baseURL() string {
	return "http://127.0.0.1:" + strconv.Itoa(t.Port)
}

// newRequest builds a request with auth and directory headers applied.
func (t Target) newRequest(ctx context.Context, method, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, t.baseURL()+path, nil)
	if err != nil {
		return nil, err
	}
	if t.User != "" || t.Pass != "" {
		req.SetBasicAuth(t.User, t.Pass)
	}
	if t.Dir != "" {
		// The SDK sends x-opencode-directory: encodeURIComponent(dir).
		req.Header.Set("x-opencode-directory", url.PathEscape(t.Dir))
	}
	return req, nil
}

// Sample is a measured statistic over a set of durations or a single scalar.
type Sample struct {
	N      int     `json:"n"`
	MinMs  float64 `json:"min_ms,omitempty"`
	P50Ms  float64 `json:"p50_ms,omitempty"`
	P99Ms  float64 `json:"p99_ms,omitempty"`
	MaxMs  float64 `json:"max_ms,omitempty"`
	MeanMs float64 `json:"mean_ms,omitempty"`
	StdMs  float64 `json:"stddev_ms,omitempty"`
}

// summarize computes order statistics over a slice of durations.
func summarize(ds []time.Duration) Sample {
	s := Sample{N: len(ds)}
	if len(ds) == 0 {
		return s
	}
	ms := make([]float64, len(ds))
	var sum float64
	for i, d := range ds {
		ms[i] = float64(d) / float64(time.Millisecond)
		sum += ms[i]
	}
	sort.Float64s(ms)
	s.MinMs = ms[0]
	s.MaxMs = ms[len(ms)-1]
	s.P50Ms = percentile(ms, 50)
	s.P99Ms = percentile(ms, 99)
	s.MeanMs = sum / float64(len(ms))
	var varSum float64
	for _, v := range ms {
		d := v - s.MeanMs
		varSum += d * d
	}
	s.StdMs = sqrt(varSum / float64(len(ms)))
	return s
}

// percentile returns the p-th percentile of a pre-sorted slice using linear
// interpolation between the two nearest ranks (the same method as NumPy's
// default and Go's gonum "linear" interpolation). Nearest-rank was rejected
// because for small n it snaps high percentiles onto the max sample by
// construction (e.g. p99 == max for n<=50), which makes the tail estimate a
// copy of the single worst observation rather than an interpolated estimate.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	// Fractional rank in [0, n-1].
	rank := (p / 100) * float64(n-1)
	if rank <= 0 {
		return sorted[0]
	}
	if rank >= float64(n-1) {
		return sorted[n-1]
	}
	lo := int(rank)
	frac := rank - float64(lo)
	return sorted[lo] + frac*(sorted[lo+1]-sorted[lo])
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// Newton's method; avoids importing math just for one call elsewhere.
	z := x
	for i := 0; i < 40; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}

// MetricResult holds the four W0 baseline metrics for one target.
type MetricResult struct {
	Target string `json:"target"`

	// ColdStartMs is per-iteration time-to-first-healthy-200.
	ColdStart Sample `json:"cold_start_ms"`
	// IdleRSSKB is the resident set size (process tree) at steady state.
	IdleRSSKB int `json:"idle_rss_kb"`
	// RSSWithSubsKB is RSS while SubConnected idle SSE subscribers are connected.
	RSSWithSubsKB int `json:"rss_with_subs_kb"`
	// SubCount is the N requested for the SSE fan-out + RSS-with-subs metrics.
	SubCount int `json:"sub_count"`
	// SubConnected is how many subscribers actually reached server.connected; the
	// RSS-with-subs and SSEConnect samples reflect THIS many live connections,
	// not SubCount. When it is < SubCount the fan-out fell short and a note is
	// recorded so the metric is never read as a full SubCount fan-out.
	SubConnected int `json:"sub_connected"`
	// SSEConnect is per-subscriber time from dial to server.connected, measured
	// with SubConnected subscribers connecting concurrently.
	SSEConnect Sample `json:"sse_connect_ms"`
	// HealthThroughput is GET /global/health requests/sec (pure router).
	HealthThroughputRPS float64 `json:"health_throughput_rps"`
	// SessionListThroughput is GET /session requests/sec (SQLite read).
	SessionListThroughputRPS float64 `json:"session_list_throughput_rps"`

	Notes []string `json:"notes,omitempty"`
}

// waitHealthy blocks until GET /global/health returns 200 or the deadline
// elapses. It returns the elapsed time from the call until the first 200.
func waitHealthy(ctx context.Context, t Target, timeout time.Duration) (time.Duration, error) {
	start := time.Now()
	deadline := start.Add(timeout)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		req, err := t.newRequest(ctx, http.MethodGet, "/global/health")
		if err != nil {
			return 0, err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return time.Since(start), nil
			}
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(15 * time.Millisecond):
		}
	}
	return 0, fmt.Errorf("%s: not healthy within %s", t.Name, timeout)
}

// spawn forks the daemon and returns the running command. The caller must Kill.
func spawn(t Target, logPath string) (*exec.Cmd, error) {
	cmd := exec.Command(t.Bin, t.Args...)
	if t.Env != nil {
		cmd.Env = t.Env
	}
	if logPath != "" {
		f, err := os.Create(logPath)
		if err != nil {
			return nil, err
		}
		cmd.Stdout = f
		cmd.Stderr = f
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// MeasureColdStart forks the daemon `iters` times and records time-to-healthy
// for each. Each iteration is a fresh process so JIT/migration warm-up is
// captured honestly (callers should pre-warm any one-time on-disk migration by
// running the daemon once against the same HOME before calling this).
func MeasureColdStart(ctx context.Context, t Target, iters int, logDir string) (Sample, error) {
	var ds []time.Duration
	for i := 0; i < iters; i++ {
		logPath := ""
		if logDir != "" {
			logPath = fmt.Sprintf("%s/%s-coldstart-%d.log", logDir, t.Name, i)
		}
		cmd, err := spawn(t, logPath)
		if err != nil {
			return Sample{}, err
		}
		d, err := waitHealthy(ctx, t, 30*time.Second)
		killTree(cmd)
		if err != nil {
			return Sample{}, fmt.Errorf("iter %d: %w", i, err)
		}
		ds = append(ds, d)
		// Let the port free up between iterations.
		time.Sleep(150 * time.Millisecond)
	}
	return summarize(ds), nil
}

// rssKB returns the resident set size in KiB for the process tree rooted at pid.
// On both darwin and linux it uses `ps`, summing the root and its descendants so
// daemons that fork helper processes are accounted for fairly.
func rssKB(pid int) (int, error) {
	// Collect pid -> ppid for all processes, then walk the tree from pid.
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,rss=").Output()
	if err != nil {
		return 0, err
	}
	type row struct{ ppid, rss int }
	rows := map[int]row{}
	children := map[int][]int{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 3 {
			continue
		}
		p, e1 := strconv.Atoi(fields[0])
		pp, e2 := strconv.Atoi(fields[1])
		r, e3 := strconv.Atoi(fields[2])
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		rows[p] = row{ppid: pp, rss: r}
		children[pp] = append(children[pp], p)
	}
	if _, ok := rows[pid]; !ok {
		return 0, fmt.Errorf("pid %d not found", pid)
	}
	total := 0
	seen := map[int]bool{}
	var walk func(int)
	walk = func(p int) {
		if seen[p] {
			return
		}
		seen[p] = true
		total += rows[p].rss
		for _, c := range children[p] {
			walk(c)
		}
	}
	walk(pid)
	return total, nil
}

// MeasureIdleRSS starts the daemon, waits for steady state, samples RSS, and
// kills it. The settle delay lets the runtime finish startup allocation and any
// lazy initialization before sampling.
func MeasureIdleRSS(ctx context.Context, t Target, settle time.Duration, logPath string) (int, error) {
	cmd, err := spawn(t, logPath)
	if err != nil {
		return 0, err
	}
	defer killTree(cmd)
	if _, err := waitHealthy(ctx, t, 30*time.Second); err != nil {
		return 0, err
	}
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(settle):
	}
	return rssKB(cmd.Process.Pid)
}

// sseConnectResult is the per-subscriber outcome of a fan-out connection.
type sseConnectResult struct {
	d   time.Duration
	err error
}

// connectSSE dials GET /event and returns the time from dial to the first
// server.connected event. It then holds the connection open until release is
// closed (so the caller can sample RSS with N live subscribers) and closes.
func connectSSE(ctx context.Context, t Target, release <-chan struct{}) (time.Duration, error) {
	start := time.Now()
	req, err := t.newRequest(ctx, http.MethodGet, "/event")
	if err != nil {
		return 0, err
	}
	// No client timeout: the stream is long-lived; ctx governs lifetime.
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("/event status %d", resp.StatusCode)
	}
	// A subscriber that gets 200 but never sees server.connected would otherwise
	// block in reader.ReadString forever — neither ctx cancellation nor closing
	// `release` unblocks a goroutine parked in Read. Close the body when ctx (or
	// release) fires so the blocked read returns an error and the goroutine
	// exits; otherwise one stuck subscriber hangs wg.Wait() until the test
	// timeout. The watcher exits via `done` on the normal return path.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
		case <-release:
		case <-done:
			return
		}
		_ = resp.Body.Close()
	}()
	reader := bufio.NewReader(resp.Body)
	connected := time.Duration(0)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if connected == 0 {
				return 0, err
			}
			return connected, nil
		}
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var ev struct {
				Type string `json:"type"`
			}
			if json.Unmarshal([]byte(payload), &ev) == nil && ev.Type == "server.connected" {
				connected = time.Since(start)
				// Hold the connection open for the RSS-with-subs sample.
				select {
				case <-release:
				case <-ctx.Done():
				}
				return connected, nil
			}
		}
	}
}

// FanoutResult is the outcome of an SSE connection fan-out measurement. It
// records the ACTUAL number of subscribers that connected (Connected) versus the
// number requested (Requested) so a shortfall can never be silently relabeled as
// a full N-subscriber fan-out.
type FanoutResult struct {
	// Requested is the N subscribers the run asked for.
	Requested int `json:"requested"`
	// Connected is how many actually reached server.connected before the
	// deadline; the RSS-with-subs sample and the latency Sample reflect exactly
	// this many live connections, not Requested.
	Connected int `json:"connected"`
	// Connect is the per-subscriber dial->server.connected latency over the
	// Connected subscribers.
	Connect Sample `json:"connect"`
	// RSSKB is the daemon process-tree RSS sampled while Connected subscribers
	// were live.
	RSSKB int `json:"rss_kb"`
}

// MeasureSSEFanout opens `n` concurrent GET /event subscriptions, records the
// per-subscriber dial->server.connected latency, samples RSS while all live
// subscribers are connected, then tears them down. If fewer than `n` connect
// within the deadline, the result records the ACTUAL connected count (it does
// not mislabel a partial fan-out as the full N) and the RSS/latency reflect that
// actual count.
func MeasureSSEFanout(ctx context.Context, t Target, n int, daemonPID int) (FanoutResult, error) {
	// release is closed once RSS-with-subs has been sampled, freeing every
	// subscriber to disconnect.
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	defer closeRelease()

	results := make([]sseConnectResult, n)
	var wg sync.WaitGroup
	var connectedCount int64
	var failedCount int64
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			d, err := connectSSE(ctx, t, release)
			results[idx] = sseConnectResult{d: d, err: err}
			if err == nil {
				atomic.AddInt64(&connectedCount, 1)
			} else {
				atomic.AddInt64(&failedCount, 1)
			}
		}(i)
	}

	// Wait until all subscribers have connected, or one fails, or the deadline
	// elapses, before sampling. Bailing on the first failure means a daemon that
	// can only hold a subset does not burn the whole 30s deadline.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&connectedCount) >= int64(n) {
			break
		}
		if atomic.LoadInt64(&failedCount) > 0 {
			break
		}
		select {
		case <-ctx.Done():
			closeRelease()
			wg.Wait()
			return FanoutResult{}, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	// Let the connections settle, then sample RSS with all live subscribers up.
	time.Sleep(500 * time.Millisecond)
	rss, rssErr := rssKB(daemonPID)

	closeRelease()
	wg.Wait()

	var ds []time.Duration
	var firstErr error
	for _, r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		ds = append(ds, r.d)
	}
	if len(ds) == 0 {
		return FanoutResult{}, fmt.Errorf("no subscribers connected: %w", firstErr)
	}
	if rssErr != nil {
		rss = 0
	}
	return FanoutResult{
		Requested: n,
		Connected: len(ds),
		Connect:   summarize(ds),
		RSSKB:     rss,
	}, nil
}

// MeasureThroughput hammers `path` with `concurrency` workers for `dur` and
// returns successful requests/sec. Non-200 responses are not counted as
// successes but do not abort the run.
//
// Two honesty guards against an upward rps bias:
//   - only completions that finish on or before the deadline are counted
//     (a request that returns after the window does not inflate the count);
//   - the rate is divided by the TRUE measured elapsed time, not the nominal
//     `dur`. The measured elapsed is the last counted completion minus the
//     start, so a window that the workers under- or over-shoot is reported at
//     its real duration.
func MeasureThroughput(ctx context.Context, t Target, path string, concurrency int, dur time.Duration) (float64, error) {
	runCtx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()

	// One shared, keep-alive-friendly transport: bound the connection pool to
	// `concurrency` per host so the load reuses sockets instead of churning
	// through ephemeral ports (which exhausts them under load on macOS).
	transport := &http.Transport{
		MaxIdleConns:        concurrency * 2,
		MaxIdleConnsPerHost: concurrency * 2,
		MaxConnsPerHost:     concurrency,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true,
	}
	defer transport.CloseIdleConnections()

	start := time.Now()
	deadline := start.Add(dur)
	var ok int64
	// lastNanos tracks the elapsed-from-start (ns) of the latest counted
	// completion, so the rate is divided by real elapsed, not the nominal dur.
	var lastNanos int64
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 5 * time.Second, Transport: transport}
			for {
				select {
				case <-runCtx.Done():
					return
				default:
				}
				req, err := t.newRequest(runCtx, http.MethodGet, path)
				if err != nil {
					return
				}
				resp, err := client.Do(req)
				if err != nil {
					if runCtx.Err() != nil {
						return
					}
					continue
				}
				// Drain so the connection can be reused.
				_, _ = drainAndDiscard(resp.Body)
				_ = resp.Body.Close()
				done := time.Now()
				// Do not count completions that landed after the window: they
				// would inflate the numerator without extending the divisor.
				if done.After(deadline) {
					return
				}
				if resp.StatusCode == http.StatusOK {
					atomic.AddInt64(&ok, 1)
					elapsed := done.Sub(start).Nanoseconds()
					for {
						prev := atomic.LoadInt64(&lastNanos)
						if elapsed <= prev || atomic.CompareAndSwapInt64(&lastNanos, prev, elapsed) {
							break
						}
					}
				}
			}
		}()
	}
	wg.Wait()

	count := atomic.LoadInt64(&ok)
	if count == 0 {
		return 0, nil
	}
	// True measured elapsed: time from start to the last counted completion.
	// Falls back to the nominal window only if the timestamp was never set
	// (cannot happen when count>0, but keeps the divisor strictly positive).
	elapsed := time.Duration(atomic.LoadInt64(&lastNanos))
	if elapsed <= 0 {
		elapsed = dur
	}
	return float64(count) / elapsed.Seconds(), nil
}

// MachineInfo describes the host the baseline was measured on. It is embedded in
// every result file so a number can never be cited without its provenance.
type MachineInfo struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	NumCPU   int    `json:"num_cpu"`
	GoVer    string `json:"go_version"`
	Hostname string `json:"hostname"`
	Model    string `json:"model,omitempty"`
}

func collectMachineInfo() MachineInfo {
	mi := MachineInfo{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		NumCPU: runtime.NumCPU(),
		GoVer:  runtime.Version(),
	}
	mi.Hostname, _ = os.Hostname()
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
			mi.Model = strings.TrimSpace(string(out))
		}
	} else {
		// Best-effort on linux: model name from /proc/cpuinfo.
		if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(line, "model name") {
					if idx := strings.Index(line, ":"); idx >= 0 {
						mi.Model = strings.TrimSpace(line[idx+1:])
						break
					}
				}
			}
		}
	}
	return mi
}

// Report is the full W0 baseline artifact written to bench/results/.
type Report struct {
	GeneratedAt string         `json:"generated_at"`
	Plan        string         `json:"plan"`
	Machine     MachineInfo    `json:"machine"`
	Config      ReportConfig   `json:"config"`
	Targets     []MetricResult `json:"targets"`
	Disclaimer  string         `json:"disclaimer"`
}

// ReportConfig records the knobs the run used so it is reproducible.
type ReportConfig struct {
	ColdStartIters    int    `json:"cold_start_iters"`
	SubCount          int    `json:"sub_count"`
	ThroughputConc    int    `json:"throughput_concurrency"`
	ThroughputSeconds int    `json:"throughput_seconds"`
	OpencodeVersion   string `json:"opencode_version,omitempty"`
	Opcode42Version   string `json:"opcode42_version,omitempty"`
}
