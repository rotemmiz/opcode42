// Package mdns advertises the daemon over multicast DNS so LAN clients (the
// mobile app, the TUI) can discover it, mirroring opencode's Bonjour publish
// (server/mdns.ts:8-44): service type _http._tcp, instance name opencode-<port>,
// TXT path=/.
package mdns

import (
	"fmt"
	"strings"

	"github.com/grandcat/zeroconf"
)

const (
	serviceType = "_http._tcp"
	domain      = "local."
)

// Service is a live mDNS advertisement; call Shutdown to withdraw it.
type Service struct {
	server *zeroconf.Server
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

// Publish advertises the daemon on port. host is the advertised hostname target
// (opencode uses "opencode.local"; mdnsDomain overrides it). It returns a
// Service whose Shutdown withdraws the record.
func Publish(port int, mdnsDomain string) (*Service, error) {
	host := strings.TrimSuffix(mdnsDomain, ".local")
	if host == "" {
		host = "opencode"
	}
	instance := fmt.Sprintf("opencode-%d", port)
	server, err := zeroconf.RegisterProxy(
		instance, serviceType, domain, port, host, nil, []string{"path=/"}, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("mdns publish: %w", err)
	}
	return &Service{server: server}, nil
}

// Shutdown withdraws the advertisement. Safe to call on a nil Service.
func (s *Service) Shutdown() {
	if s != nil && s.server != nil {
		s.server.Shutdown()
		s.server = nil
	}
}
