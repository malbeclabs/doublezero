package liveness

import (
	"net"
	"sync"
	"time"
)

// ifCache caches network interface index-to-name and name-to-index mappings
// to avoid expensive RTM_GETLINK netlink dumps on every packet.
type ifCache struct {
	mu        sync.RWMutex
	byIndex   map[int]string
	byName    map[string]int
	updatedAt time.Time
	ttl       time.Duration
}

func newIfCache(ttl time.Duration) *ifCache {
	return &ifCache{
		ttl: ttl,
	}
}

func (c *ifCache) refresh() {
	ifaces, err := net.Interfaces()
	if err != nil {
		return
	}
	byIndex := make(map[int]string, len(ifaces))
	byName := make(map[string]int, len(ifaces))
	for _, ifi := range ifaces {
		byIndex[ifi.Index] = ifi.Name
		byName[ifi.Name] = ifi.Index
	}
	c.mu.Lock()
	c.byIndex = byIndex
	c.byName = byName
	c.updatedAt = time.Now()
	c.mu.Unlock()
}

func (c *ifCache) maybeRefresh() {
	c.mu.RLock()
	stale := time.Since(c.updatedAt) > c.ttl
	c.mu.RUnlock()
	if stale {
		c.refresh()
	}
}

// NameByIndex returns the interface name for the given index.
// Returns "" if not found (even after a forced refresh for a cache miss).
func (c *ifCache) NameByIndex(idx int) string {
	c.maybeRefresh()
	c.mu.RLock()
	name, ok := c.byIndex[idx]
	c.mu.RUnlock()
	if ok {
		return name
	}
	// Cache miss: a new interface may have appeared. Force one refresh.
	c.refresh()
	c.mu.RLock()
	name = c.byIndex[idx]
	c.mu.RUnlock()
	return name
}

// IndexByName returns the interface index for the given name.
// Returns 0 and an error if not found after a forced refresh.
func (c *ifCache) IndexByName(name string) (int, bool) {
	c.maybeRefresh()
	c.mu.RLock()
	idx, ok := c.byName[name]
	c.mu.RUnlock()
	if ok {
		return idx, true
	}
	c.refresh()
	c.mu.RLock()
	idx, ok = c.byName[name]
	c.mu.RUnlock()
	return idx, ok
}
