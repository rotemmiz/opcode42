package mdns

import (
	"context"
	"net"
	"strings"
	"sync"

	"github.com/grandcat/zeroconf"
)

// DiscoveredService is a resolved mDNS advertisement surfaced by Browse. It is
// the client-side mirror of the daemon's Publish record: instance name (e.g.
// "opcode42-4096"), a dialable host:port, and the TXT record. The Host is
// resolved to a concrete IP when available (AddrIPv4/AddrIPv6 on the
// underlying zeroconf.ServiceEntry); it falls back to the service's HostName
// (e.g. "opencode.local") when no address was resolved in time.
type DiscoveredService struct {
	Name string
	Host string
	Port int
	TXT  []string
}

// DefaultBrowseServiceTypes are the service types Browse listens on when the
// caller does not supply its own. They mirror the daemon's dual-publish
// (mdns.go: httpServiceType + opcode42ServiceType) and Android F3's parallel
// browse (plan 15 §F3): _http._tcp catches opencode serve --mdns and the
// daemon's http alias; _opencode._tcp catches the daemon's brand record.
var DefaultBrowseServiceTypes = []string{"_http._tcp.", "_opencode._tcp."}

// DefaultBrowseNamePrefixes are the instance-name prefixes Browse admits when
// the caller does not supply its own. The daemon names instances
// "opencode-<port>" (mdns.go:63) and Opcode42-aware forks use "opcode42-<port>"
// (plan 08e §D1). Non-matching services on the same service type (printers,
// AirPlay, random HTTP servers) are dropped.
var DefaultBrowseNamePrefixes = []string{"opencode-", "opcode42-"}

// Browse discovers Opcode42/opencode daemons on the LAN by browsing serviceType
// in parallel and resolving each entry. It emits one DiscoveredService per
// unique (host, port) on the returned channel; the channel is closed when all
// browsers stop (ctx cancel or resolver error).
//
// serviceTypes defaults to DefaultBrowseServiceTypes; namePrefixes defaults to
// DefaultBrowseNamePrefixes. An entry is admitted only if its instance name
// has one of the prefixes. The same daemon advertised on both _http._tcp and
// _opencode._tcp (the daemon's dual-publish) is deduped to a single
// DiscoveredService — the first resolution wins.
//
// Browse is safe to cancel via ctx: canceling the caller's context stops every
// resolver and closes the output channel. The caller MUST drain the channel
// until it closes (or select on ctx.Done and abandon the goroutine).
func Browse(ctx context.Context, serviceTypes []string, namePrefixes []string) (<-chan DiscoveredService, error) {
	if len(serviceTypes) == 0 {
		serviceTypes = DefaultBrowseServiceTypes
	}
	if len(namePrefixes) == 0 {
		namePrefixes = DefaultBrowseNamePrefixes
	}

	out := make(chan DiscoveredService)
	var wg sync.WaitGroup
	var dedupeMu sync.Mutex
	seen := make(map[hostPortKey]struct{})

	for _, st := range serviceTypes {
		st := st
		wg.Add(1)
		go func() {
			defer wg.Done()
			browseOne(ctx, st, namePrefixes, out, &dedupeMu, seen)
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out, nil
}

type hostPortKey struct {
	host string
	port int
}

func browseOne(ctx context.Context, serviceType string, namePrefixes []string, out chan<- DiscoveredService, mu *sync.Mutex, seen map[hostPortKey]struct{}) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return
	}

	entries := make(chan *zeroconf.ServiceEntry)
	if err := resolver.Browse(ctx, serviceType, "local.", entries); err != nil {
		return
	}

	for entry := range entries {
		if !matchesPrefix(entry.Instance, namePrefixes) {
			continue
		}
		host := pickHost(entry)
		if host == "" {
			continue
		}
		key := hostPortKey{host: host, port: entry.Port}
		mu.Lock()
		if _, ok := seen[key]; ok {
			mu.Unlock()
			continue
		}
		seen[key] = struct{}{}
		mu.Unlock()

		svc := DiscoveredService{
			Name: entry.Instance,
			Host: host,
			Port: entry.Port,
			TXT:  append([]string(nil), entry.Text...),
		}
		select {
		case out <- svc:
		case <-ctx.Done():
			return
		}
	}
}

func matchesPrefix(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func pickHost(entry *zeroconf.ServiceEntry) string {
	if len(entry.AddrIPv4) > 0 {
		return entry.AddrIPv4[0].String()
	}
	if len(entry.AddrIPv6) > 0 {
		return entry.AddrIPv6[0].String()
	}
	if entry.HostName != "" {
		return normalizeHost(entry.HostName)
	}
	return ""
}

func normalizeHost(h string) string {
	h = strings.TrimSuffix(h, ".")
	if ip := net.ParseIP(h); ip != nil {
		return ip.String()
	}
	return h
}
