package geoprobe

import (
	"sync"
	"time"
)

// UpdateResult describes what happened to the incoming value in a MinCache.Update call.
type UpdateResult int

const (
	UpdateNone   UpdateResult = iota // incoming value was discarded
	UpdateBest                       // incoming value became the new best
	UpdateBackup                     // incoming value became the new backup
)

func (r UpdateResult) String() string {
	switch r {
	case UpdateNone:
		return "no_change"
	case UpdateBest:
		return "new_best"
	case UpdateBackup:
		return "backup_updated"
	default:
		return "unknown"
	}
}

// UpdateInfo is the result of a MinCache.Update call. It describes what happened
// to the incoming value and whether a backup-to-best promotion occurred during
// this call (independent of the incoming value's placement).
type UpdateInfo struct {
	Result UpdateResult
	// Promoted is true if backup was promoted to best because the old best expired.
	Promoted bool
	// PrevBestRttNs is the RTT of the previous best before this update, captured
	// inside the lock. Zero if there was no previous best.
	PrevBestRttNs uint64
	HadPrevBest   bool
}

// Changed returns true if the best value changed, either from a new best
// measurement or a backup promotion.
func (u UpdateInfo) Changed() bool {
	return u.Result == UpdateBest || u.Promoted
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
func (c *MinCache[T]) Update(value T) UpdateInfo {
	c.mu.Lock()
	defer c.mu.Unlock()

	rttNs := c.rttFunc(value)
	now := c.nowFunc()
	entry := &minEntry[T]{
		value:      value,
		rttNs:      rttNs,
		receivedAt: now,
	}

	// Capture previous best before any mutation.
	var info UpdateInfo
	if !c.best.expiredAt(now, c.maxAge) {
		info.PrevBestRttNs = c.best.rttNs
		info.HadPrevBest = true
	}

	if c.best.expiredAt(now, c.maxAge) {
		if !c.backup.expiredAt(now, c.maxAge) {
			info.PrevBestRttNs = c.best.rttNs // expired best's RTT (before promotion)
			info.HadPrevBest = c.best != nil
			c.best = c.backup
			info.Promoted = true
		} else {
			c.best = nil
		}
		c.backup = nil
	}

	if c.best == nil {
		c.best = entry
		info.Result = UpdateBest
		return info
	}

	if rttNs <= c.best.rttNs {
		c.best = entry
		info.Result = UpdateBest
		return info
	}

	// Higher RTT than best — consider for backup.
	if c.backup.expiredAt(now, c.maxAge) || rttNs <= c.backup.rttNs {
		c.backup = entry
		info.Result = UpdateBackup
		return info
	}
	halfMaxAge := c.maxAge / 2
	if now.Sub(c.backup.receivedAt) > halfMaxAge {
		c.backup = entry
		info.Result = UpdateBackup
		return info
	}

	return info
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

// Empty returns true if both best and backup are expired.
func (c *MinCache[T]) Empty() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	now := c.nowFunc()
	return c.best.expiredAt(now, c.maxAge) && c.backup.expiredAt(now, c.maxAge)
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

// Sweep removes caches where both best and backup have expired.
func (m *MinCacheMap[K, V]) Sweep() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, c := range m.caches {
		if c.Empty() {
			delete(m.caches, k)
		}
	}
}
