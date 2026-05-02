package geoprobe

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"
)

// DeliveryDNSRefresher runs periodic DNS resolution for result-destination host:port
// strings off the measurement loop. The hot path uses DNSCache.LookupDeliveryUDPAddr
// only (no DNS). Refreshes run at startup, when the desired set changes, and on a
// fixed ticker (aligned with DNS cache TTL).
type DeliveryDNSRefresher struct {
	cache   *DNSCache
	log     *slog.Logger
	mu      sync.Mutex
	desired map[string]struct{}

	trigger chan struct{}
}

// NewDeliveryDNSRefresher builds a refresher that owns a DNSCache with the given TTL.
func NewDeliveryDNSRefresher(log *slog.Logger, ttl time.Duration) *DeliveryDNSRefresher {
	if log == nil {
		log = slog.Default()
	}
	return &DeliveryDNSRefresher{
		cache:   NewDNSCache(ttl),
		log:     log,
		desired: make(map[string]struct{}),
		trigger: make(chan struct{}, 1),
	}
}

// Cache returns the underlying DNS cache for tests.
func (r *DeliveryDNSRefresher) Cache() *DNSCache {
	return r.cache
}

// Lookup resolves delivery addresses without blocking on DNS for hostnames.
func (r *DeliveryDNSRefresher) Lookup(hostPort string) (*net.UDPAddr, bool) {
	return r.cache.LookupDeliveryUDPAddr(hostPort)
}

// SetDesiredHostPorts replaces the set of host:port strings that should stay resolved.
// Triggers a coalesced background refresh (non-blocking).
func (r *DeliveryDNSRefresher) SetDesiredHostPorts(hostPorts []string) {
	r.mu.Lock()
	r.desired = make(map[string]struct{}, len(hostPorts))
	for _, h := range hostPorts {
		if h != "" {
			r.desired[h] = struct{}{}
		}
	}
	r.mu.Unlock()

	select {
	case r.trigger <- struct{}{}:
	default:
	}
}

// Start runs refresh immediately, then on each refreshInterval tick and whenever
// SetDesiredHostPorts signals. Blocks until ctx is canceled.
func (r *DeliveryDNSRefresher) Start(ctx context.Context, refreshInterval time.Duration) {
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	r.refresh()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refresh()
		case <-r.trigger:
			r.refresh()
		}
	}
}

func (r *DeliveryDNSRefresher) refresh() {
	r.mu.Lock()
	hostPorts := make([]string, 0, len(r.desired))
	for h := range r.desired {
		hostPorts = append(hostPorts, h)
	}
	r.mu.Unlock()

	for _, hp := range hostPorts {
		if _, err := r.cache.Resolve(hp); err != nil {
			r.log.Warn("delivery DNS refresh failed", "hostPort", hp, "error", err)
		}
	}
}
