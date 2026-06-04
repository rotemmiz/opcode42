// Package mdns advertises the daemon over multicast DNS so LAN clients (the
// mobile app, the TUI) can discover it. opencode publishes a single Bonjour
// record on _http._tcp (server/mdns.ts:8-44): instance name opencode-<port>,
// TXT path=/. Forge advertises that record verbatim (so unmodified opencode
// clients and generic _http._tcp browsers still find it) AND a richer
// _opencode._tcp record carrying auth + version TXT keys (plan 13 §Discovery)
// that Forge-aware clients prefer.
package mdns

import (
	"fmt"
	"strings"

	"github.com/grandcat/zeroconf"
)

const (
	// httpServiceType is opencode's wire-compatible service type (mdns.ts:21).
	httpServiceType = "_http._tcp"
	// forgeServiceType is Forge's richer record carrying auth/version TXT keys
	// (plan 13 §Discovery: "advertise _opencode._tcp ... also advertise
	// _http._tcp as an alias"). Forge-aware clients prefer this one.
	forgeServiceType = "_opencode._tcp"
	domain           = "local."
)

// Service is a live mDNS advertisement; call Shutdown to withdraw it. Forge
// publishes two records (one per service type) behind a single Service.
type Service struct {
	servers []*zeroconf.Server
}

// ShouldPublish reports whether mDNS should be advertised: enabled AND bound to
// a non-loopback hostname (server/server.ts:158-164). Publishing on a loopback
// address is pointless, so opencode skips it with a warning.
func ShouldPublish(enabled bool, hostname string) bool {
	return enabled && !IsLoopback(hostname)
}

// IsLoopback reports whether hostname is a loopback address opencode refuses to
// advertise on (server.ts:160).
func IsLoopback(hostname string) bool {
	switch strings.ToLower(strings.TrimSpace(hostname)) {
	case "127.0.0.1", "localhost", "::1", "":
		return true
	default:
		return false
	}
}

// Publish advertises the daemon on port over both _http._tcp (opencode-compat)
// and _opencode._tcp (Forge-aware). mdnsDomain overrides the advertised host
// (opencode uses "opencode.local"); authRequired controls the auth TXT key on
// the Forge record. It returns a Service whose Shutdown withdraws both records.
//
// If the second (Forge) record fails to register, the first is rolled back so a
// partial advertisement is never left running.
func Publish(port int, mdnsDomain string, authRequired bool) (*Service, error) {
	host := strings.TrimSuffix(mdnsDomain, ".local")
	if host == "" {
		host = "opencode"
	}
	instance := fmt.Sprintf("opencode-%d", port)

	// opencode-compatible record: _http._tcp with TXT path=/ verbatim (mdns.ts:21).
	httpSrv, err := zeroconf.RegisterProxy(
		instance, httpServiceType, domain, port, host, nil, []string{"path=/"}, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("mdns publish %s: %w", httpServiceType, err)
	}

	// Forge-aware record: _opencode._tcp carrying auth + version TXT keys.
	authVal := "open"
	if authRequired {
		authVal = "required"
	}
	forgeTXT := []string{"path=/", "auth=" + authVal, "version=1"}
	forgeSrv, err := zeroconf.RegisterProxy(
		instance, forgeServiceType, domain, port, host, nil, forgeTXT, nil,
	)
	if err != nil {
		httpSrv.Shutdown()
		return nil, fmt.Errorf("mdns publish %s: %w", forgeServiceType, err)
	}

	return &Service{servers: []*zeroconf.Server{httpSrv, forgeSrv}}, nil
}

// Shutdown withdraws every advertisement. Safe to call on a nil Service.
func (s *Service) Shutdown() {
	if s == nil {
		return
	}
	for _, srv := range s.servers {
		if srv != nil {
			srv.Shutdown()
		}
	}
	s.servers = nil
}
