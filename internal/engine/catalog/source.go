package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultURL is the models.dev catalog endpoint.
const DefaultURL = "https://models.dev/api.json"

// DefaultTTL is how long a fetched catalog is considered fresh before a refetch
// is attempted, matching opencode's 5-minute disk freshness window
// (models-dev.ts:145).
const DefaultTTL = 5 * time.Minute

// Source resolves the model catalog. Implementations are safe for concurrent use.
type Source interface {
	Get(ctx context.Context) (Catalog, error)
}

// Static is a fixed in-memory catalog, used by tests (the checked-in fixture) so
// they never touch the network.
type Static struct{ Catalog Catalog }

// NewStatic wraps a fixed catalog as a Source.
func NewStatic(c Catalog) *Static { return &Static{Catalog: c} }

// Get returns the static catalog.
func (s *Static) Get(context.Context) (Catalog, error) { return s.Catalog, nil }

// Live fetches the catalog from models.dev with an on-disk cache and a
// last-good offline fallback: a fetch failure returns the cached copy (even if
// stale) rather than erroring, so a models.dev outage never bricks startup.
type Live struct {
	URL        string
	CachePath  string
	TTL        time.Duration
	HTTPClient *http.Client

	mu        sync.Mutex
	cached    Catalog
	fetchedAt time.Time
}

// NewLive builds a live source with defaults filled in. cachePath "" uses the
// per-user cache dir.
func NewLive(cachePath string) *Live {
	if cachePath == "" {
		cachePath = defaultCachePath()
	}
	return &Live{URL: DefaultURL, CachePath: cachePath, TTL: DefaultTTL, HTTPClient: http.DefaultClient}
}

// Get returns the catalog, preferring a fresh in-memory or on-disk copy, then a
// live fetch, then any stale cached copy as an offline fallback.
func (l *Live) Get(ctx context.Context) (Catalog, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.cached != nil && time.Since(l.fetchedAt) < l.ttl() {
		return l.cached, nil
	}

	// Fresh disk cache short-circuits the network.
	if cat, mtime, err := l.loadDisk(); err == nil && time.Since(mtime) < l.ttl() {
		l.cached, l.fetchedAt = cat, time.Now()
		return cat, nil
	}

	cat, err := l.fetch(ctx)
	if err != nil {
		// Offline fallback: serve the last good cache (memory, then disk).
		if l.cached != nil {
			return l.cached, nil
		}
		if disk, _, derr := l.loadDisk(); derr == nil {
			l.cached, l.fetchedAt = disk, time.Now()
			return disk, nil
		}
		return nil, err
	}
	l.cached, l.fetchedAt = cat, time.Now()
	return cat, nil
}

func (l *Live) ttl() time.Duration {
	if l.TTL <= 0 {
		return DefaultTTL
	}
	return l.TTL
}

func (l *Live) fetch(ctx context.Context) (Catalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "opcode42/0.0.1")
	client := l.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models.dev: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch models.dev: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	cat, err := Parse(body)
	if err != nil {
		return nil, err
	}
	l.writeDisk(body) // best-effort
	return cat, nil
}

func (l *Live) loadDisk() (Catalog, time.Time, error) {
	if l.CachePath == "" {
		return nil, time.Time{}, os.ErrNotExist
	}
	info, err := os.Stat(l.CachePath)
	if err != nil {
		return nil, time.Time{}, err
	}
	body, err := os.ReadFile(l.CachePath)
	if err != nil {
		return nil, time.Time{}, err
	}
	cat, err := Parse(body)
	if err != nil {
		return nil, time.Time{}, err
	}
	return cat, info.ModTime(), nil
}

func (l *Live) writeDisk(body []byte) {
	if l.CachePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(l.CachePath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(l.CachePath, body, 0o644)
}

// Parse decodes a models.dev api.json document into a Catalog.
func Parse(body []byte) (Catalog, error) {
	var cat Catalog
	if err := json.Unmarshal(body, &cat); err != nil {
		return nil, fmt.Errorf("parse models.dev catalog: %w", err)
	}
	return cat, nil
}

func defaultCachePath() string {
	if p := os.Getenv("OPCODE_MODELS_CACHE"); p != "" {
		return p
	}
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cacheHome = filepath.Join(home, ".cache")
		}
	}
	return filepath.Join(cacheHome, "opcode42", "models.json")
}
