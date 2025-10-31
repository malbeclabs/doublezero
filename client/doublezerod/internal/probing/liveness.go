package probing

import "sync"

// LivenessStatus represents the current reachability of a route.
type LivenessStatus uint8

const (
	LivenessStatusUnknown LivenessStatus = iota // initial/indeterminate
	LivenessStatusUp                            // route considered alive
	LivenessStatusDown                          // route considered dead
)

// LivenessTransition indicates whether a probe caused a status change.
type LivenessTransition int

const (
	LivenessTransitionNoChange LivenessTransition = iota // no transition
	LivenessTransitionToUp                               // transitioned to Up
	LivenessTransitionToDown                             // transitioned to Down
)

// LivenessTracker maintains per-route probe history and state transitions.
type LivenessTracker interface {
	OnProbe(ok bool) LivenessTransition // update tracker with probe result and return transition type
	Status() LivenessStatus             // current up/down/unknown status
	ConsecutiveOK() uint                // consecutive successful probes
	ConsecutiveFail() uint              // consecutive failed probes
}

// LivenessPolicy creates new trackers; it encapsulates the hysteresis parameters.
type LivenessPolicy interface {
	NewTracker() LivenessTracker
}

// livenessState holds probe counters and current status.
type livenessState struct {
	consecOK, consecFail uint
	status               LivenessStatus
}

// hysteresisTracker implements LivenessTracker with threshold-based transitions.
// It requires N successive OKs to mark a route up, and M successive fails to mark it down.
type hysteresisTracker struct {
	s    livenessState
	up   uint
	down uint
	mu   sync.RWMutex
}

// OnProbe updates consecutive probe counts and determines if a state transition occurred.
func (t *hysteresisTracker) OnProbe(ok bool) LivenessTransition {
	t.mu.Lock()
	defer t.mu.Unlock()

	if ok {
		t.s.consecOK++
		t.s.consecFail = 0
	} else {
		t.s.consecFail++
		t.s.consecOK = 0
	}

	want := t.s.status
	if t.s.consecOK >= t.up {
		want = LivenessStatusUp
	}
	if t.s.consecFail >= t.down {
		want = LivenessStatusDown
	}
	if want == t.s.status {
		return LivenessTransitionNoChange
	}
	t.s.status = want
	if want == LivenessStatusUp {
		return LivenessTransitionToUp
	}
	return LivenessTransitionToDown
}

// Status returns the current liveness status.
func (t *hysteresisTracker) Status() LivenessStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.s.status
}

// ConsecutiveOK returns the number of consecutive successful probes.
func (t *hysteresisTracker) ConsecutiveOK() uint {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.s.consecOK
}

// ConsecutiveFail returns the number of consecutive failed probes.
func (t *hysteresisTracker) ConsecutiveFail() uint {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.s.consecFail
}

// NewHysteresisLivenessPolicy constructs a policy with given up/down thresholds.
// up: number of consecutive OK probes to mark up
// down: number of consecutive fails to mark down
func NewHysteresisLivenessPolicy(up, down uint) LivenessPolicy {
	return &HysteresisPolicy{
		UpThreshold:   up,
		DownThreshold: down,
	}
}

// HysteresisPolicy defines thresholds for up/down transitions.
type HysteresisPolicy struct {
	UpThreshold   uint
	DownThreshold uint
}

// NewTracker returns a new hysteresisTracker initialized to Down state.
func (p *HysteresisPolicy) NewTracker() LivenessTracker {
	return &hysteresisTracker{
		s:    livenessState{status: LivenessStatusDown},
		up:   p.UpThreshold,
		down: p.DownThreshold,
	}
}
