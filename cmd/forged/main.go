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
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rotemmiz/forge/internal/auth"
	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/config"
	"github.com/rotemmiz/forge/internal/engine"
	"github.com/rotemmiz/forge/internal/engine/catalog"
	"github.com/rotemmiz/forge/internal/engine/llm"
	"github.com/rotemmiz/forge/internal/engine/message"
	"github.com/rotemmiz/forge/internal/engine/provider/anthropic"
	"github.com/rotemmiz/forge/internal/engine/provider/openai"
	"github.com/rotemmiz/forge/internal/engine/registry"
	"github.com/rotemmiz/forge/internal/engine/tool"
	"github.com/rotemmiz/forge/internal/instance"
	"github.com/rotemmiz/forge/internal/mdns"
	"github.com/rotemmiz/forge/internal/oauth"
	"github.com/rotemmiz/forge/internal/pluginbridge"
	"github.com/rotemmiz/forge/internal/server"
	"github.com/rotemmiz/forge/internal/session"
	"github.com/rotemmiz/forge/internal/storage"
	"github.com/rotemmiz/forge/internal/websearch"
)

// version is the daemon version, overridable at build time via
// -ldflags "-X main.version=...".
var version = "0.0.1"

// options holds the resolved network settings (flags merged with config).
type options struct {
	host          string
	port          int
	mdns          bool
	mdnsDomain    string
	oauthProxyURL string
	// pluginHost enables the flag-gated opencode-plugin sidecar (plan 05). Off
	// by default; never affects the default daemon path.
	pluginHost bool
}

func main() {
	host := flag.String("host", "127.0.0.1", "host/interface to bind")
	port := flag.Int("port", 4096, "HTTP listen port (0 = OS-assigned)")
	enableMDNS := flag.Bool("mdns", false, "advertise the daemon over mDNS")
	mdnsDomain := flag.String("mdns-domain", "", "mDNS host (default opencode.local)")
	oauthProxyURL := flag.String("oauth-callback-proxy-url", "",
		"externally reachable base URL fronting the loopback OAuth callback (remote/headless daemons); "+
			"empty = loopback-only")
	pluginHost := flag.Bool("plugin-host", false, "enable the opencode-plugin host sidecar (plan 05; off by default)")
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
		host:          *host,
		port:          *port,
		mdns:          *enableMDNS,
		mdnsDomain:    *mdnsDomain,
		oauthProxyURL: *oauthProxyURL,
		pluginHost:    *pluginHost || os.Getenv("FORGE_PLUGIN_HOST") == "1",
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

	modelCatalog := loadCatalog(baseCtx)
	todos := tool.NewTodoStore()

	// Provider OAuth service: the loopback callback server + built-in OAuth
	// providers (plan 13). A bad --oauth-callback-proxy-url is a hard start error.
	oauthSvc, err := oauth.NewService(opts.oauthProxyURL)
	if err != nil {
		return err
	}

	handler, err := server.New(server.Options{
		Version:   version,
		Auth:      authCfg,
		Cwd:       cwd,
		Sessions:  session.NewStore(db),
		Instances: instances,
		Global:    globalBus,
		BaseCtx:   baseCtx,
		Messages:  message.NewStore(db),
		Catalog:   modelCatalog,
		Registry:  builtinRegistry(todos),
		Todos:     todos,
		Providers: providerFactory(modelCatalog),
		OAuth:     oauthSvc,
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

	// Register the flag-gated plugin host factory (plan 05). The bridge's SDK
	// client needs the bound server URL + auth header, both known only now.
	if opts.pluginHost {
		registerPluginHost(baseCtx, instances, ln.Addr().String(), authCfg)
	}

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
		return shutdown(srv, instances, mdnsSvc, oauthSvc, cancelBase)
	}
}

// registerPluginHost wires the per-instance plugin host factory onto the
// instance manager (plan 05). Each new instance gets a bridge configured with
// the directory, the bound server URL, and the Basic auth header so plugins'
// SDK clients can call back into this daemon. The bridge is started here; a
// start failure is non-fatal (the bridge stays a no-op and the instance runs
// plugin-free). When the flag is off this function is never called.
func registerPluginHost(baseCtx context.Context, instances *instance.Manager, addr string, authCfg auth.Config) {
	serverURL := "http://" + addr
	authHeader := ""
	if authCfg.Required() {
		cred := base64.StdEncoding.EncodeToString([]byte(authCfg.Username + ":" + authCfg.Password))
		authHeader = "Basic " + cred
	}
	instances.SetPluginFactory(func(directory string) *pluginbridge.Bridge {
		cfg, _ := config.Load(directory)
		b := pluginbridge.New(pluginbridge.Config{
			Enabled:     true,
			Directory:   directory,
			ServerURL:   serverURL,
			AuthHeader:  authHeader,
			PluginSpecs: pluginbridge.ConfigSpecs(cfg),
		})
		// Start in the background so instance creation never blocks on the
		// sidecar boot; hook call sites no-op until host.ready arrives.
		go func() { _ = b.Start(baseCtx) }()
		return b
	})
	log.Printf("plugin host enabled (plan 05); plugins load per instance")
}

// loadCatalog resolves the models.dev catalog at startup (live, with on-disk
// cache), falling back to the embedded fixture if the fetch fails so the daemon
// always starts. Cost accuracy for unknown models degrades gracefully to 0.
func loadCatalog(ctx context.Context) catalog.Catalog {
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cat, err := catalog.NewLive("").Get(fetchCtx)
	if err != nil {
		log.Printf("warning: models.dev catalog unavailable (%v); using bundled fixture", err)
		return catalog.Fixture()
	}
	return cat
}

// builtinRegistry is the agent's built-in tool set. MCP/config tools fill the
// registry's dynamic slot in plan 03. The todo store is passed in so the daemon
// can also serve it over GET /session/:id/todo.
func builtinRegistry(todos *tool.TodoStore) *registry.Registry {
	return registry.New(
		tool.Bash{}, tool.Read{}, tool.Write{}, tool.Edit{}, tool.Glob{}, tool.Grep{}, tool.Patch{},
		tool.WebFetch{}, tool.TodoWrite{Store: todos}, tool.Question{}, tool.Task{}, tool.Skill{},
		tool.WebSearch{Searcher: websearch.New()}, tool.LSP{},
	)
}

// providerFactory builds a streaming client for a provider/model. It routes to
// the Anthropic client for Anthropic-native providers (by id or catalog npm) and
// the OpenAI-compatible client otherwise, resolving the base URL from the catalog
// (or FORGE_PROVIDER_BASE_URL) and the API key from the provider's advertised env
// vars (or FORGE_PROVIDER_API_KEY).
func providerFactory(cat catalog.Catalog) engine.ProviderFactory {
	return func(_ context.Context, providerID, modelID string) (llm.Provider, error) {
		baseURL := os.Getenv("FORGE_PROVIDER_BASE_URL")
		var apiKey, npm string
		if prov, ok := cat[providerID]; ok {
			if baseURL == "" {
				baseURL = prov.API
			}
			apiKey = firstEnv(prov.Env...)
			npm = prov.NPM
		}
		if apiKey == "" {
			apiKey = os.Getenv("FORGE_PROVIDER_API_KEY")
		}
		if baseURL == "" {
			baseURL = builtinBaseURL(providerID)
		}
		if baseURL == "" {
			return nil, fmt.Errorf("no base URL for provider %q (set FORGE_PROVIDER_BASE_URL)", providerID)
		}
		if isAnthropic(providerID, npm) {
			return anthropic.New(anthropic.Options{BaseURL: baseURL, APIKey: apiKey, Model: modelID}), nil
		}
		return openai.New(openai.Options{BaseURL: baseURL, APIKey: apiKey, Model: modelID}), nil
	}
}

// builtinBaseURL returns an OpenAI-compatible base URL for providers whose
// models.dev catalog entry advertises no `api` field but that nonetheless expose
// an OpenAI-compatible endpoint. opencode reaches these through provider-specific
// AI-SDK packages (e.g. @ai-sdk/google) with the endpoint baked in; Forge's
// OpenAI-compatible client needs the URL explicitly. The api key still comes from
// the provider's advertised env vars (GEMINI_API_KEY / GOOGLE_*_API_KEY for
// google), so only the base URL is supplied here. FORGE_PROVIDER_BASE_URL still
// takes precedence (it is resolved before this fallback).
//
// Reference: Gemini's OpenAI-compatibility layer lives at
// https://generativelanguage.googleapis.com/v1beta/openai/ and accepts the same
// POST {base}/chat/completions the openai client emits.
func builtinBaseURL(providerID string) string {
	switch providerID {
	case "google":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	default:
		return ""
	}
}

// isAnthropic reports whether a provider speaks the Anthropic Messages API
// natively. Bedrock/Vertex host Anthropic models but over different wire formats
// and auth (SigV4 / GCP OAuth, not x-api-key), so they are deliberately excluded
// — they'd need their own clients.
func isAnthropic(providerID, npm string) bool {
	if strings.Contains(providerID, "bedrock") || strings.Contains(providerID, "vertex") {
		return false
	}
	return providerID == "anthropic" || strings.Contains(npm, "anthropic")
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
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
func shutdown(srv *http.Server, instances *instance.Manager, mdnsSvc *mdns.Service, oauthSvc *oauth.Service, cancelBase context.CancelFunc) error {
	fmt.Fprintln(os.Stderr, "forged: shutting down")
	mdnsSvc.Shutdown()
	instances.DisposeAll()
	cancelBase()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if oauthSvc != nil {
		oauthSvc.Shutdown(shutdownCtx)
	}
	return srv.Shutdown(shutdownCtx)
}
