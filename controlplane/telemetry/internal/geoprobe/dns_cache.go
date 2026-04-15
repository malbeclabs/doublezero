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

	// If host is already an IP, use directly.
	if ip := net.ParseIP(host); ip != nil {
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

	c.mu.Lock()
	c.entries[host] = dnsCacheEntry{
		ip:        resolved,
		expiresAt: now.Add(c.ttl),
	}
	c.mu.Unlock()

	return &net.UDPAddr{IP: net.ParseIP(resolved), Port: port}, nil
}
