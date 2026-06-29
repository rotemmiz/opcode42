// Command opcoded is the Opcode42 daemon: a ground-up, interop-first alternative
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

	"github.com/rotemmiz/opcode42/internal/auth"
	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/config"
	"github.com/rotemmiz/opcode42/internal/engine"
	"github.com/rotemmiz/opcode42/internal/engine/catalog"
	"github.com/rotemmiz/opcode42/internal/engine/llm"
	"github.com/rotemmiz/opcode42/internal/engine/message"
	"github.com/rotemmiz/opcode42/internal/engine/provider/anthropic"
	"github.com/rotemmiz/opcode42/internal/engine/provider/credresolve"
	"github.com/rotemmiz/opcode42/internal/engine/provider/openai"
	"github.com/rotemmiz/opcode42/internal/engine/registry"
	"github.com/rotemmiz/opcode42/internal/engine/tool"
	"github.com/rotemmiz/opcode42/internal/instance"
	"github.com/rotemmiz/opcode42/internal/mdns"
	"github.com/rotemmiz/opcode42/internal/oauth"
	"github.com/rotemmiz/opcode42/internal/pluginbridge"
	"github.com/rotemmiz/opcode42/internal/push"
	"github.com/rotemmiz/opcode42/internal/server"
	"github.com/rotemmiz/opcode42/internal/session"
	"github.com/rotemmiz/opcode42/internal/storage"
	"github.com/rotemmiz/opcode42/internal/websearch"
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
	// fcmServiceAccount is the path to a Google service-account JSON key enabling
	// the push relay (plan 13 §13.8). Empty ⇒ push relay runs in no-op mode
	// (device registration persists but no FCM send is attempted). Resolved from
	// --fcm-service-account or OPCODE_FCM_SERVICE_ACCOUNT.
	fcmServiceAccount string
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
	fcmServiceAccount := flag.String("fcm-service-account", "",
		"path to a Google service-account JSON key enabling FCM push (plan 13 §13.8); "+
			"empty = push relay disabled (registration still persists)")
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
		host:              *host,
		port:              *port,
		mdns:              *enableMDNS,
		mdnsDomain:        *mdnsDomain,
		oauthProxyURL:     *oauthProxyURL,
		fcmServiceAccount: firstNonEmpty(*fcmServiceAccount, os.Getenv("OPCODE_FCM_SERVICE_ACCOUNT")),
		pluginHost:        *pluginHost || os.Getenv("OPCODE_PLUGIN_HOST") == "1",
	}, explicit)

	if err := run(opts); err != nil {
		log.Fatalf("opcoded: %v", err)
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
		// opencode only warns when the server is unsecured (cli/cmd/serve.ts:15);
		// Opcode42 keeps the warning on loopback but REFUSES to expose an
		// unauthenticated daemon on a non-loopback interface (plan 13 §"Defaults":
		// "0.0.0.0 bind requires a password; daemon refuses to start otherwise").
		if err := authCfg.CheckBindExposure(opts.host); err != nil {
			return err
		}
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

	// sessions publishes its lifecycle events (session.created/updated/deleted)
	// to the per-directory instance bus so SSE subscribers see them.
	sessions := session.NewStore(db).WithBus(func(directory string) session.EventPublisher {
		return instances.BusFor(directory)
	})

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

	// Push relay (plan 13 §13.8). The device-registration store is always wired
	// so a mobile client can register before FCM is configured; the relay only
	// sends when a service account is present (no-op mode otherwise). A bad
	// service-account key is a hard start error so misconfiguration is loud.
	pushStore := push.NewStore(db.DB)
	pushRelay, err := newPushRelay(opts.fcmServiceAccount, pushStore, globalBus)
	if err != nil {
		return err
	}
	go pushRelay.Run(baseCtx)

	handler, err := server.New(server.Options{
		Version:   version,
		Auth:      authCfg,
		Cwd:       cwd,
		Sessions:  sessions,
		Instances: instances,
		Global:    globalBus,
		BaseCtx:   baseCtx,
		Messages:  message.NewStore(db),
		Catalog:   modelCatalog,
		Registry:  builtinRegistry(todos),
		Todos:     todos,
		Providers: providerFactory(modelCatalog, oauthSvc),
		OAuth:     oauthSvc,
		Push:      pushStore,
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

	mdnsSvc := startMDNS(opts, ln, authCfg.Required())
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

// newPushRelay builds the FCM push relay (plan 13 §13.8). When saPath is empty
// the relay runs in no-op mode (no FCM send; device registration still
// persists). When set, the service-account JSON is loaded and parsed eagerly so
// a misconfigured key fails startup loudly rather than silently disabling push.
func newPushRelay(saPath string, store *push.Store, global *bus.Global) (*push.Relay, error) {
	if saPath == "" {
		return push.NewRelay(store, nil, global, nil), nil
	}
	saJSON, err := os.ReadFile(saPath)
	if err != nil {
		return nil, fmt.Errorf("read FCM service account %q: %w", saPath, err)
	}
	sender, err := push.NewFCMSender(saJSON)
	if err != nil {
		return nil, fmt.Errorf("FCM service account %q: %w", saPath, err)
	}
	return push.NewRelay(store, sender, global, nil), nil
}

// firstNonEmpty returns the first non-empty string among its arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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
// (or OPCODE_PROVIDER_BASE_URL).
//
// Credential resolution prefers a provider's OAuth access token over the static
// API-key path, mirroring opencode's per-provider auth loader (xai.ts:575-660):
// a provider signed in via OAuth has its access token refreshed (when stale) and
// injected as Authorization: Bearer (xai.ts:657), overriding the env-var/api-key
// credential; a provider with no OAuth record uses the API-key path unchanged
// (xai.ts:596). credresolve.Resolve performs that branch via oauthSvc.Access;
// oauthSvc may be nil (OAuth disabled), in which case only the API-key path runs.
func providerFactory(cat catalog.Catalog, oauthSvc credresolve.Accessor) engine.ProviderFactory {
	return func(ctx context.Context, providerID, modelID string) (llm.Provider, error) {
		baseURL := os.Getenv("OPCODE_PROVIDER_BASE_URL")
		var apiKey, npm string
		if prov, ok := cat[providerID]; ok {
			if baseURL == "" {
				baseURL = prov.API
			}
			apiKey = firstEnv(prov.Env...)
			npm = prov.NPM
		}
		if apiKey == "" {
			apiKey = os.Getenv("OPCODE_PROVIDER_API_KEY")
		}
		if baseURL == "" {
			baseURL = builtinBaseURL(providerID)
		}
		if baseURL == "" {
			return nil, fmt.Errorf("no base URL for provider %q (set OPCODE_PROVIDER_BASE_URL)", providerID)
		}

		// Resolve the credential: OAuth access token first, else the static API
		// key. An ErrNeedsReauth (stored token expired, refresh failed) surfaces
		// here so the request fails with a re-auth signal instead of calling the
		// provider with a dead credential.
		cred, err := credresolve.Resolve(ctx, oauthSvc, providerID, apiKey)
		if err != nil {
			return nil, fmt.Errorf("resolve credentials for provider %q: %w", providerID, err)
		}

		if isAnthropic(providerID, npm) {
			opts := anthropic.Options{BaseURL: baseURL, Model: modelID}
			if cred.OAuth {
				// Anthropic OAuth (Claude Pro/Max) authenticates with
				// Authorization: Bearer, not x-api-key — opencode swaps the header
				// for OAuth-backed Anthropic the same way it does for xAI.
				opts.Headers = map[string]string{"authorization": "Bearer " + cred.APIKey}
			} else {
				opts.APIKey = cred.APIKey
			}
			return anthropic.New(opts), nil
		}
		// OpenAI-compatible client (covers xAI, Opcode42's only OAuth provider today):
		// APIKey is emitted as Authorization: Bearer, so an OAuth access token and
		// a static api key take the same code path here.
		return openai.New(openai.Options{BaseURL: baseURL, APIKey: cred.APIKey, Model: modelID}), nil
	}
}

// builtinBaseURL returns an OpenAI-compatible base URL for providers whose
// models.dev catalog entry advertises no `api` field but that nonetheless expose
// an OpenAI-compatible endpoint. opencode reaches these through provider-specific
// AI-SDK packages (e.g. @ai-sdk/google) with the endpoint baked in; Opcode42's
// OpenAI-compatible client needs the URL explicitly. The api key still comes from
// the provider's advertised env vars (GEMINI_API_KEY / GOOGLE_*_API_KEY for
// google), so only the base URL is supplied here. OPCODE_PROVIDER_BASE_URL still
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
// enabled and the host is not loopback (server.ts:158-164). It publishes both
// the opencode-compatible _http._tcp record and Opcode42's richer _opencode._tcp
// record (plan 13 §Discovery); authRequired sets the auth TXT key on the latter.
// Returns nil when nothing was published.
func startMDNS(opts options, ln net.Listener, authRequired bool) *mdns.Service {
	if !opts.mdns {
		return nil
	}
	if !mdns.ShouldPublish(true, opts.host) {
		log.Printf("warning: mDNS enabled but host %q is loopback; skipping mDNS publish", opts.host)
		return nil
	}
	port := ln.Addr().(*net.TCPAddr).Port
	svc, err := mdns.Publish(port, opts.mdnsDomain, authRequired)
	if err != nil {
		log.Printf("warning: mDNS publish failed: %v", err)
		return nil
	}
	log.Printf("mDNS: advertising opencode-%d via _http._tcp.local and _opencode._tcp.local", port)
	return svc
}

// shutdown runs the graceful shutdown sequence (plan 01 §9): withdraw mDNS,
// dispose instances (emits server.instance.disposed, kills PTYs), unblock the
// long-lived streams, then drain HTTP with a 10s deadline. SQLite is closed by
// run's deferred db.Close.
func shutdown(srv *http.Server, instances *instance.Manager, mdnsSvc *mdns.Service, oauthSvc *oauth.Service, cancelBase context.CancelFunc) error {
	fmt.Fprintln(os.Stderr, "opcoded: shutting down")
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
