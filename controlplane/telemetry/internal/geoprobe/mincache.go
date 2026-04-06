package geoprobe

import (
	"sync"
	"time"
)

// UpdateResult describes how a MinCache.Update call changed the cache state.
type UpdateResult int

const (
	UpdateNone     UpdateResult = iota // no change to best or backup
	UpdateBest                         // best was replaced with a new lower-RTT value
	UpdateBackup                       // backup was replaced
	UpdatePromoted                     // backup was promoted to best (old best expired)
)

func (r UpdateResult) String() string {
	switch r {
	case UpdateNone:
		return "no_change"
	case UpdateBest:
		return "new_best"
	case UpdateBackup:
		return "backup_updated"
	case UpdatePromoted:
		return "promoted"
	default:
		return "unknown"
	}
}

// Changed returns true if the update resulted in a new best value, either
// from a direct replacement or a backup promotion.
func (r UpdateResult) Changed() bool {
	return r == UpdateBest || r == UpdatePromoted
}

type minEntry[T any] struct {
	value      T
	rttNs      uint64
	receivedAt time.Time
}

func (e *minEntry[T]) expiredAt(now time.Time, maxAge time.Duration) bool {
	return e == nil || now.Sub(e.receivedAt) > maxAge
}

// MinCache tracks the minimum-RTT value over a rolling TTL window using a
// best/backup pattern. When best expires, backup is promoted. This is a
// single-stream simplification of the agent's offsetCache.
type MinCache[T any] struct {
	mu      sync.RWMutex
	best    *minEntry[T]
	backup  *minEntry[T]
	maxAge  time.Duration
	rttFunc func(T) uint64
	nowFunc func() time.Time // for testing; defaults to time.Now
}

func NewMinCache[T any](maxAge time.Duration, rttFunc func(T) uint64) *MinCache[T] {
	return &MinCache[T]{
		maxAge:  maxAge,
		rttFunc: rttFunc,
		nowFunc: time.Now,
	}
}

// Update feeds a new measurement into the cache and returns what changed.
func (c *MinCache[T]) Update(value T) UpdateResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	rttNs := c.rttFunc(value)
	now := c.nowFunc()
	entry := &minEntry[T]{
		value:      value,
		rttNs:      rttNs,
		receivedAt: now,
	}

	promoted := false
	if c.best.expiredAt(now, c.maxAge) {
		if !c.backup.expiredAt(now, c.maxAge) {
			c.best = c.backup
			promoted = true
		} else {
			c.best = nil
		}
		c.backup = nil
	}

	if c.best == nil {
		c.best = entry
		return UpdateBest
	}

	if rttNs <= c.best.rttNs {
		// New measurement beats current best (whether it was promoted or original).
		c.best = entry
		return UpdateBest
	}

	// Higher RTT than best — consider for backup.
	if c.backup.expiredAt(now, c.maxAge) || rttNs <= c.backup.rttNs {
		c.backup = entry
		if promoted {
			return UpdatePromoted
		}
		return UpdateBackup
	}
	halfMaxAge := c.maxAge / 2
	if now.Sub(c.backup.receivedAt) > halfMaxAge {
		c.backup = entry
		if promoted {
			return UpdatePromoted
		}
		return UpdateBackup
	}

	if promoted {
		return UpdatePromoted
	}
	return UpdateNone
}

// Best returns the current best value. If best is expired, it falls through to
// backup without mutating state (no actual promotion). Use Update to trigger
// promotion events.
func (c *MinCache[T]) Best() (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := c.nowFunc()
	if !c.best.expiredAt(now, c.maxAge) {
		return c.best.value, true
	}
	if !c.backup.expiredAt(now, c.maxAge) {
		return c.backup.value, true
	}
	var zero T
	return zero, false
}

// BestRttNs returns the RTT of the current best entry, falling through to
// backup if best is expired (same read-through semantics as Best).
func (c *MinCache[T]) BestRttNs() (uint64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := c.nowFunc()
	if !c.best.expiredAt(now, c.maxAge) {
		return c.best.rttNs, true
	}
	if !c.backup.expiredAt(now, c.maxAge) {
		return c.backup.rttNs, true
	}
	return 0, false
}

// MinCacheMap manages per-key MinCache instances, creating them on demand.
type MinCacheMap[K comparable, V any] struct {
	mu      sync.RWMutex
	caches  map[K]*MinCache[V]
	maxAge  time.Duration
	rttFunc func(V) uint64
}

func NewMinCacheMap[K comparable, V any](maxAge time.Duration, rttFunc func(V) uint64) *MinCacheMap[K, V] {
	return &MinCacheMap[K, V]{
		caches:  make(map[K]*MinCache[V]),
		maxAge:  maxAge,
		rttFunc: rttFunc,
	}
}

// Get returns the cache for a key, creating one if it doesn't exist.
func (m *MinCacheMap[K, V]) Get(key K) *MinCache[V] {
	m.mu.RLock()
	c, ok := m.caches[key]
	m.mu.RUnlock()
	if ok {
		return c
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok = m.caches[key]; ok {
		return c
	}
	c = NewMinCache[V](m.maxAge, m.rttFunc)
	m.caches[key] = c
	return c
}
