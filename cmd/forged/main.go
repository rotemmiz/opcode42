// Command forged is the Forge daemon: a ground-up, interop-first alternative
// to opencode that is wire-compatible with its HTTP+SSE+WebSocket API.
//
// The S4 skeleton serves GET /global/health and GET /doc and returns 501 for
// every other documented operation — just enough to be a conformance dual-run
// target. Transport/state/auth/routing (M1–M7) is plan 01.
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

	"github.com/rotemmiz/forge/internal/server"
)

// version is the daemon version, overridable at build time via
// -ldflags "-X main.version=...".
var version = "0.0.1"

func main() {
	port := flag.Int("port", 4096, "HTTP listen port (0 = OS-assigned)")
	host := flag.String("host", "127.0.0.1", "host/interface to bind")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if err := run(*host, *port); err != nil {
		log.Fatalf("forged: %v", err)
	}
}

func run(host string, port int) error {
	handler, err := server.New(server.Options{Version: version})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
		WriteTimeout:      0, // SSE streams are long-lived (plan 01 §1)
		IdleTimeout:       120 * time.Second,
	}

	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	// opencode prints this exact prefix; clients scrape it for the bound port.
	log.Printf("opencode server listening on http://%s", ln.Addr().String())

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
		fmt.Fprintln(os.Stderr, "forged: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
