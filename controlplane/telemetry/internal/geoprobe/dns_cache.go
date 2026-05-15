package geoprobe

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

// DNSCache resolves host:port strings to *net.UDPAddr, caching DNS lookups
// for domain-based hosts with a configurable TTL.
type DNSCache struct {
	mu      sync.RWMutex
	entries map[string]dnsCacheEntry
	ttl     time.Duration
	now     func() time.Time               // for testing
	lookup  func(string) ([]string, error) // for testing
}

type dnsCacheEntry struct {
	ip        string
	expiresAt time.Time
}

// NewDNSCache creates a DNSCache with the given TTL for cached lookups.
func NewDNSCache(ttl time.Duration) *DNSCache {
	return &DNSCache{
		entries: make(map[string]dnsCacheEntry),
		ttl:     ttl,
		now:     time.Now,
		lookup:  net.LookupHost,
	}
}

// Resolve resolves a host:port string to a *net.UDPAddr.
// If the host is an IP address, it is used directly (no caching).
// If the host is a domain name, DNS lookup is performed and the result
// is cached for the configured TTL.
func (c *DNSCache) Resolve(hostPort string) (*net.UDPAddr, error) {
	host, portStr, err := net.SplitHostPort(hostPort)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: %w", hostPort, err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port in %q: %w", hostPort, err)
	}

	// If host is already an IP, validate scope and use directly.
	if ip := net.ParseIP(host); ip != nil {
		scopeCheck := ProbeAddress{Host: host}
		if err := scopeCheck.ValidateScope(); err != nil {
			return nil, fmt.Errorf("delivery address rejected: %w", err)
		}
		return &net.UDPAddr{IP: ip, Port: port}, nil
	}

	// Domain name — check cache.
	now := c.now()

	c.mu.RLock()
	entry, ok := c.entries[host]
	c.mu.RUnlock()

	if ok && now.Before(entry.expiresAt) {
		return &net.UDPAddr{IP: net.ParseIP(entry.ip), Port: port}, nil
	}

	// Cache miss or expired — resolve.
	ips, err := c.lookup(host)
	if err != nil {
		return nil, fmt.Errorf("DNS lookup failed for %q: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("DNS lookup returned no results for %q", host)
	}

	resolved := ips[0]
	resolvedIP := net.ParseIP(resolved)
	if resolvedIP == nil {
		return nil, fmt.Errorf("DNS lookup for %q returned unparseable IP %q", host, resolved)
	}

	// Validate resolved IP against scope check to prevent DNS rebinding attacks
	// (e.g., domain initially resolves to public IP but later rebinds to internal).
	scopeCheck := ProbeAddress{Host: resolved}
	if err := scopeCheck.ValidateScope(); err != nil {
		return nil, fmt.Errorf("DNS-resolved address for %q rejected: %w", host, err)
	}

	c.mu.Lock()
	c.entries[host] = dnsCacheEntry{
		ip:        resolved,
		expiresAt: now.Add(c.ttl),
	}
	c.mu.Unlock()

	return &net.UDPAddr{IP: resolvedIP, Port: port}, nil
}

// LookupDeliveryUDPAddr returns a UDP address for delivery without performing DNS.
// Literal IPs are parsed and validated against ValidateScope on every call (cheap).
// Hostnames are only satisfied from the in-memory cache populated by Resolve (e.g.
// from DeliveryDNSRefresher); if missing or expired, returns ok=false so callers can
// skip sending until a background refresh succeeds.
func (c *DNSCache) LookupDeliveryUDPAddr(hostPort string) (*net.UDPAddr, bool) {
	host, portStr, err := net.SplitHostPort(hostPort)
	if err != nil {
		return nil, false
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, false
	}

	if ip := net.ParseIP(host); ip != nil {
		scopeCheck := ProbeAddress{Host: host}
		if err := scopeCheck.ValidateScope(); err != nil {
			return nil, false
		}
		return &net.UDPAddr{IP: ip, Port: port}, true
	}

	now := c.now()

	c.mu.RLock()
	entry, ok := c.entries[host]
	c.mu.RUnlock()

	if !ok || !now.Before(entry.expiresAt) {
		return nil, false
	}

	resolvedIP := net.ParseIP(entry.ip)
	if resolvedIP == nil {
		return nil, false
	}

	scopeCheck := ProbeAddress{Host: entry.ip}
	if err := scopeCheck.ValidateScope(); err != nil {
		return nil, false
	}

	return &net.UDPAddr{IP: resolvedIP, Port: port}, true
}
