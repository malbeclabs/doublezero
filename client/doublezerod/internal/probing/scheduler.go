package probing

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sync"
	"time"
)

// ProbeOutcome records the result of a completed probe, including timing
// and any associated error, used by the scheduler to re-arm future probes.
type ProbeOutcome struct {
	OK   bool
	RTT  time.Duration
	Err  error
	When time.Time
}

// Scheduler determines when routes should be probed.
// Implementations manage timing, backoff, jitter, and rescheduling policies.
type Scheduler interface {
	Add(key RouteKey, base time.Time)
	Del(key RouteKey) bool
	PopDue(now time.Time) []RouteKey
	Peek() (time.Time, bool)
	Complete(key RouteKey, outcome ProbeOutcome)
	Len() int
	Clear()
	String() string
	Wake() <-chan struct{}
}

// IntervalConfig defines the interval, jitter, and optional per-route phasing
// used by an IntervalScheduler.
type IntervalConfig struct {
	Interval time.Duration // base interval between probes
	Jitter   time.Duration // max absolute jitter (+/-) applied to the interval/phase anchor
	Phase    bool          // whether to phase routes deterministically
	NowFunc  NowFunc       // function to get the current time
}

// Validate ensures the configuration is usable.
func (cfg *IntervalConfig) Validate() error {
	if cfg.Interval <= 0 {
		return errors.New("interval must be > 0")
	}
	if cfg.Jitter < 0 {
		return errors.New("jitter must be >= 0")
	}
	if cfg.NowFunc == nil {
		cfg.NowFunc = func() time.Time {
			return time.Now().UTC()
		}
	}
	return nil
}

// IntervalScheduler runs probes at fixed intervals, optionally jittered
// and/or phase-shifted per route key. Each route is rearmed independently
// when its probe completes.
type IntervalScheduler struct {
	mu     sync.Mutex
	cfg    IntervalConfig
	routes map[RouteKey]*schedulerRouteState

	// wake/broadcast state
	wake    chan struct{} // closed to signal worker to recheck
	lastDue time.Time     // last observed earliest due
	hadAny  bool          // last non-empty state
}

// schedulerRouteState tracks per-route schedule state.
type schedulerRouteState struct {
	seed    uint64
	nextDue time.Time
}

// NewIntervalScheduler creates a scheduler with a fixed interval and optional
// jitter and deterministic phase offset.
func NewIntervalScheduler(interval time.Duration, jitter time.Duration, phase bool) (Scheduler, error) {
	cfg := IntervalConfig{Interval: interval, Jitter: jitter, Phase: phase}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &IntervalScheduler{
		cfg:    cfg,
		routes: make(map[RouteKey]*schedulerRouteState),
		wake:   make(chan struct{}),
	}, nil
}

// String returns a descriptive name for the scheduler.
func (s *IntervalScheduler) String() string {
	return fmt.Sprintf("IntervalScheduler(interval=%s, jitter=%s, phase=%t)", s.cfg.Interval, s.cfg.Jitter, s.cfg.Phase)
}

// Wake returns a channel closed when the earliest due time or route set changes.
func (s *IntervalScheduler) Wake() <-chan struct{} { return s.wake }

// ---- core API ----

// Add registers a route with its initial due time based on base and config.
// If Phase is enabled, the offset is derived deterministically from the key.
func (s *IntervalScheduler) Add(k RouteKey, base time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.routes[k]; ok {
		return
	}
	seed := hash64(k)
	due := firstDue(base, s.cfg.Interval, s.cfg.Jitter, s.cfg.Phase, k, seed, s.cfg.NowFunc)
	s.routes[k] = &schedulerRouteState{seed: seed, nextDue: due}
	s.maybeSignalLocked()
}

// Del removes a route from scheduling, returning true if it existed.
func (s *IntervalScheduler) Del(k RouteKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.routes[k]; !ok {
		return false
	}
	delete(s.routes, k)
	s.maybeSignalLocked()
	return true
}

// PopDue returns all routes whose nextDue ≤ now and marks them in-flight.
// Each must later call Complete() to re-arm.
func (s *IntervalScheduler) PopDue(now time.Time) (out []RouteKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, st := range s.routes {
		if !st.nextDue.IsZero() && !st.nextDue.After(now) {
			out = append(out, k)
			st.nextDue = time.Time{} // mark as in-flight
		}
	}
	// No signal here; Complete() will signal after re-arming.
	return
}

// Peek returns the earliest nextDue across all routes, if any exist.
func (s *IntervalScheduler) Peek() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var min time.Time
	first := true
	for _, st := range s.routes {
		if st.nextDue.IsZero() { // in-flight
			continue
		}
		if first || st.nextDue.Before(min) {
			min = st.nextDue
			first = false
		}
	}
	if first {
		return time.Time{}, false
	}
	return min, true
}

// Complete re-arms the given route for its next probe time based on interval,
// jitter, and probe completion timestamp.
func (s *IntervalScheduler) Complete(k RouteKey, outcome ProbeOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.routes[k]
	if st == nil {
		return
	}
	delay := jittered(s.cfg.Interval, s.cfg.Jitter, st.seed^uint64(outcome.When.UnixNano()))
	st.nextDue = outcome.When.Add(delay)
	s.maybeSignalLocked()
}

// Len returns the number of routes currently tracked by the scheduler.
func (s *IntervalScheduler) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.routes)
}

// Clear removes all routes and signals any waiters that scheduling changed.
func (s *IntervalScheduler) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes = make(map[RouteKey]*schedulerRouteState)
	s.hadAny = false
	s.lastDue = time.Time{}
	s.signalLocked()
}

// ---- signaling helpers ----

// signalLocked closes the current wake channel and creates a new one,
// notifying waiters that they should recheck Peek().
func (s *IntervalScheduler) signalLocked() {
	old := s.wake
	s.wake = make(chan struct{})
	close(old)
}

// maybeSignalLocked checks if the earliest due or emptiness changed
// and triggers signalLocked if so.
func (s *IntervalScheduler) maybeSignalLocked() {
	// Compute current earliest and non-empty state.
	var earliest time.Time
	have := false
	for _, st := range s.routes {
		if st.nextDue.IsZero() {
			continue
		} // in-flight
		if !have || st.nextDue.Before(earliest) {
			earliest = st.nextDue
			have = true
		}
	}

	// Fire if emptiness changed, or earliest changed.
	shouldSignal := have != s.hadAny || (have && !earliest.Equal(s.lastDue))
	s.hadAny = have
	s.lastDue = earliest
	if shouldSignal {
		s.signalLocked()
	}
}

// ---- utilities ----

// hash64 derives a deterministic seed from the route key for phase/jitter.
func hash64(k RouteKey) uint64 {
	h := fnv.New64a()
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(k.table))
	h.Write(b[:])
	h.Write(k.dst.AsSlice())
	h.Write(k.nextHop.AsSlice())
	return h.Sum64()
}

// phaseOffset produces a deterministic offset within [0, interval)
// used when Phase is enabled.
func phaseOffset(iv time.Duration, k RouteKey) time.Duration {
	if iv <= 0 {
		return 0
	}
	u := hash64(k) % 1_000_000
	f := float64(u) / 1e6
	return time.Duration(f * float64(iv))
}

// firstDue computes the initial due time for a route given the config.
// If Phase is enabled, routes are staggered deterministically by key.
func firstDue(base time.Time, iv time.Duration, jitter time.Duration, phase bool, k RouteKey, seed uint64, nowFunc NowFunc) time.Time {
	if base.IsZero() {
		base = nowFunc()
	}
	if phase {
		d := base.Truncate(iv).Add(phaseOffset(iv, k))
		if d.Before(base) {
			d = base
		}
		if jitter > 0 {
			return d.Add(jittered(0, jitter, seed))
		}
		return d
	}
	return base.Add(jittered(iv, jitter, seed))
}

// jittered applies absolute jitter to an interval duration using
// the given seed for reproducible randomness. Result is clamped at >= 0.
func jittered(iv time.Duration, jitter time.Duration, seed uint64) time.Duration {
	if jitter <= 0 {
		return iv
	}
	r := rand.New(rand.NewSource(int64(seed)))
	offset := time.Duration((r.Float64()*2 - 1) * float64(jitter))
	res := iv + offset
	if res < 0 {
		res = 0
	}
	return res
}
