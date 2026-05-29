// Command forged is the Forge daemon: a ground-up, interop-first alternative
// to opencode that is wire-compatible with its HTTP+SSE+WebSocket API.
//
// It serves GET /global/health, GET /doc, GET /config, session CRUD, the SSE
// event streams, and PTY-over-WebSocket, with the opencode-compatible auth +
// directory middleware chain; remaining documented operations return a
// structured 501. mDNS advertising and graceful shutdown follow plan 01 §9.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/rotemmiz/forge/internal/auth"
	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/config"
	"github.com/rotemmiz/forge/internal/instance"
	"github.com/rotemmiz/forge/internal/mdns"
	"github.com/rotemmiz/forge/internal/server"
	"github.com/rotemmiz/forge/internal/session"
	"github.com/rotemmiz/forge/internal/storage"
)

// version is the daemon version, overridable at build time via
// -ldflags "-X main.version=...".
var version = "0.0.1"

// options holds the resolved network settings (flags merged with config).
type options struct {
	host       string
	port       int
	mdns       bool
	mdnsDomain string
}

func main() {
	host := flag.String("host", "127.0.0.1", "host/interface to bind")
	port := flag.Int("port", 4096, "HTTP listen port (0 = OS-assigned)")
	enableMDNS := flag.Bool("mdns", false, "advertise the daemon over mDNS")
	mdnsDomain := flag.String("mdns-domain", "", "mDNS host (default opencode.local)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	// Which flags the operator set explicitly — config overrides only unset ones
	// (cli/network.ts:44-61).
	explicit := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })

	opts := resolveOptions(options{
		host:       *host,
		port:       *port,
		mdns:       *enableMDNS,
		mdnsDomain: *mdnsDomain,
	}, explicit)

	if err := run(opts); err != nil {
		log.Fatalf("forged: %v", err)
	}
}

// resolveOptions merges config.server over the flags for any flag the operator
// did not set explicitly (cli/network.ts:44-61). When mDNS is on and no
// hostname is configured, the bind host defaults to 0.0.0.0 so the service is
// reachable on the LAN.
func resolveOptions(o options, explicit map[string]bool) options {
	// Network settings come from the GLOBAL config (config.Load("") skips the
	// project layer), matching opencode's getGlobal() (cli/network.ts:40).
	cfg, err := config.Load("")
	if err != nil {
		log.Printf("warning: failed to load config for network settings: %v", err)
		return o
	}
	s := config.Server(cfg)

	if !explicit["port"] && s.Port != nil {
		o.port = *s.Port
	}
	if !explicit["mdns"] && s.MDNS != nil {
		o.mdns = *s.MDNS
	}
	if !explicit["mdns-domain"] && s.MDNSDomain != nil {
		o.mdnsDomain = *s.MDNSDomain
	}
	if !explicit["host"] {
		switch {
		case s.Hostname != nil:
			o.host = *s.Hostname
		case o.mdns:
			o.host = "0.0.0.0"
		}
	}
	return o
}

func run(opts options) error {
	authCfg := auth.FromEnv()
	if !authCfg.Required() {
		// opencode warns when the server is unsecured (cli/cmd/serve.ts:15).
		log.Printf("warning: OPENCODE_SERVER_PASSWORD is not set; server is unauthenticated")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}

	db, err := storage.Open(storage.DefaultPath())
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer func() { _ = db.Close() }()

	globalBus := bus.NewGlobal()
	instances := instance.NewManager(globalBus)

	// baseCtx is cancelled at the start of shutdown to unblock SSE/PTY streams.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()

	handler, err := server.New(server.Options{
		Version:   version,
		Auth:      authCfg,
		Cwd:       cwd,
		Sessions:  session.NewStore(db),
		Instances: instances,
		Global:    globalBus,
		BaseCtx:   baseCtx,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
		WriteTimeout:      0, // SSE/PTY streams are long-lived (plan 01 §1)
		IdleTimeout:       120 * time.Second,
	}

	ln, err := net.Listen("tcp", net.JoinHostPort(opts.host, strconv.Itoa(opts.port)))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	// opencode prints this exact prefix; clients scrape it for the bound port.
	log.Printf("opencode server listening on http://%s", ln.Addr().String())

	mdnsSvc := startMDNS(opts, ln)
	if mdnsSvc != nil {
		defer mdnsSvc.Shutdown()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return shutdown(srv, instances, mdnsSvc, cancelBase)
	}
}

// startMDNS advertises the service on the actually-bound port when mDNS is
// enabled and the host is not loopback (server.ts:158-164). Returns nil when
// nothing was published.
func startMDNS(opts options, ln net.Listener) *mdns.Service {
	if !opts.mdns {
		return nil
	}
	if !mdns.ShouldPublish(true, opts.host) {
		log.Printf("warning: mDNS enabled but host %q is loopback; skipping mDNS publish", opts.host)
		return nil
	}
	port := ln.Addr().(*net.TCPAddr).Port
	svc, err := mdns.Publish(port, opts.mdnsDomain)
	if err != nil {
		log.Printf("warning: mDNS publish failed: %v", err)
		return nil
	}
	log.Printf("mDNS: advertising opencode-%d via _http._tcp.local", port)
	return svc
}

// shutdown runs the graceful shutdown sequence (plan 01 §9): withdraw mDNS,
// dispose instances (emits server.instance.disposed, kills PTYs), unblock the
// long-lived streams, then drain HTTP with a 10s deadline. SQLite is closed by
// run's deferred db.Close.
func shutdown(srv *http.Server, instances *instance.Manager, mdnsSvc *mdns.Service, cancelBase context.CancelFunc) error {
	fmt.Fprintln(os.Stderr, "forged: shutting down")
	mdnsSvc.Shutdown()
	instances.DisposeAll()
	cancelBase()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
