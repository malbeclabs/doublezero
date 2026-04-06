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

type minEntry[T any] struct {
	value      T
	rttNs      uint64
	receivedAt time.Time
}

func (e *minEntry[T]) expired(maxAge time.Duration) bool {
	return e == nil || time.Since(e.receivedAt) > maxAge
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
}

func NewMinCache[T any](maxAge time.Duration, rttFunc func(T) uint64) *MinCache[T] {
	return &MinCache[T]{
		maxAge:  maxAge,
		rttFunc: rttFunc,
	}
}

// Update feeds a new measurement into the cache and returns what changed.
func (c *MinCache[T]) Update(value T) UpdateResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	rttNs := c.rttFunc(value)
	now := time.Now()
	entry := &minEntry[T]{
		value:      value,
		rttNs:      rttNs,
		receivedAt: now,
	}

	promoted := false
	if c.best.expired(c.maxAge) {
		if !c.backup.expired(c.maxAge) {
			c.best = c.backup
			promoted = true
		} else {
			c.best = nil
		}
		c.backup = nil
	}

	if c.best == nil {
		c.best = entry
		if promoted {
			return UpdatePromoted
		}
		return UpdateBest
	}

	if rttNs <= c.best.rttNs {
		c.best = entry
		if promoted {
			return UpdatePromoted
		}
		return UpdateBest
	}

	// Higher RTT than best — consider for backup.
	if c.backup.expired(c.maxAge) || rttNs <= c.backup.rttNs {
		c.backup = entry
		if promoted {
			return UpdatePromoted
		}
		return UpdateBackup
	}
	halfMaxAge := c.maxAge / 2
	if time.Since(c.backup.receivedAt) > halfMaxAge {
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

// Best returns the current best value if non-expired.
func (c *MinCache[T]) Best() (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.best.expired(c.maxAge) {
		return c.best.value, true
	}
	if !c.backup.expired(c.maxAge) {
		return c.backup.value, true
	}
	var zero T
	return zero, false
}

// BestRttNs returns the RTT of the current best entry, or 0 if none.
func (c *MinCache[T]) BestRttNs() (uint64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.best.expired(c.maxAge) {
		return c.best.rttNs, true
	}
	if !c.backup.expired(c.maxAge) {
		return c.backup.rttNs, true
	}
	return 0, false
}
