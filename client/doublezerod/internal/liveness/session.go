package liveness

import (
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

// Session models a single bidirectional liveness relationship with a peer,
// maintaining BFD-like state, timers, and randomized transmission scheduling.
type Session struct {
	route *routing.Route

	localDiscr, peerDiscr uint32 // discriminators identify this session to each side
	state                 State  // current BFD state

	// detectMult scales the detection timeout relative to the receive interval;
	// it defines how many consecutive RX intervals may elapse without traffic
	// before declaring the session Down (e.g., 3 → tolerate ~3 missed intervals).
	detectMult uint8

	localTxMin, localRxMin time.Duration // our minimum TX/RX intervals
	peerTxMin, peerRxMin   time.Duration // peer's advertised TX/RX intervals

	nextTx, detectDeadline, lastRx time.Time // computed next transmit time, detect timeout, last RX time

	peer     *Peer
	peerAddr *net.UDPAddr

	alive bool // manager lifecycle flag: whether this session is still managed

	minTxFloor, maxTxCeil time.Duration // global interval bounds
	backoffMax            time.Duration // upper bound for exponential backoff
	backoffFactor         uint32        // doubles when Down, resets when Up

	// Convergence time tracking.
	convUpStart   time.Time // first valid RX while Down
	convDownStart time.Time // first failed/missing RX while Up (or explicit Down RX)

	mu sync.Mutex // guards mutable session state

	// Scheduled time of the next enqueued detect and tx events (zero means nothing enqueued)
	nextTxScheduled     time.Time
	nextDetectScheduled time.Time
}

// GetState returns the current state of the session.
func (s *Session) GetState() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// ComputeNextTx picks the next transmit time based on current state,
// applies exponential backoff when Down, adds ±10% jitter,
// persists it to s.nextTx, and returns the chosen timestamp.
func (s *Session) ComputeNextTx(now time.Time, rnd *rand.Rand) time.Time {
	s.mu.Lock()

	base := s.txInterval()
	eff := base
	if s.state == StateDown {
		if s.backoffFactor < 1 {
			s.backoffFactor = 1
		}
		eff *= time.Duration(s.backoffFactor)
		if s.backoffMax > 0 && eff > s.backoffMax {
			eff = s.backoffMax
		}
	}

	j := eff / 10
	span := int64(2*j) + 1
	if span < 1 {
		span = 1
	}
	var off int64
	if rnd != nil {
		off = rnd.Int63n(span)
	} else {
		off = rand.Int63n(span)
	}
	jit := time.Duration(off) - j
	next := now.Add(eff + jit)
	s.nextTx = next

	// Backoff doubles while Down; reset once Up or Init again.
	if s.state == StateDown {
		if s.backoffMax == 0 || eff < s.backoffMax {
			if s.backoffFactor == 0 {
				s.backoffFactor = 1
			}
			s.backoffFactor *= 2
		}
	} else {
		s.backoffFactor = 1
	}
	s.mu.Unlock()
	return next
}

// ArmDetect ensures the detection timer is active and not stale.
// If expired, it re-arms; if uninitialized, it returns false.
// Returns the deadline and whether detect should be (re)scheduled.
func (s *Session) ArmDetect(now time.Time) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.alive || s.detectDeadline.IsZero() {
		return time.Time{}, false
	}
	ddl := s.detectDeadline
	if !ddl.After(now) {
		ddl = now.Add(s.detectTime())
		s.detectDeadline = ddl
	}
	return ddl, true
}

// ExpireIfDue transitions an active session to Down if its detect timer
// has elapsed. Returns true if state changed (Up/Init → Down).
func (s *Session) ExpireIfDue(now time.Time) (expired bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.alive {
		return false
	}

	if (s.state == StateUp || s.state == StateInit) &&
		!s.detectDeadline.IsZero() &&
		!now.Before(s.detectDeadline) {

		// If we were Up, the first "missing" moment is when the next RX should
		// have arrived after the last successful RX.
		if s.state == StateUp && s.convDownStart.IsZero() {
			start := s.lastRx.Add(s.rxInterval())
			if now.After(start) {
				s.convDownStart = start
			} else {
				s.convDownStart = now
			}
		}

		s.state = StateDown
		s.backoffFactor = 1
		s.detectDeadline = time.Time{}
		return true
	}
	return false
}

// HandleRx ingests an incoming control packet, validates discriminators,
// updates peer timers, re-arms detection, and performs state transitions
// according to a simplified BFD-like handshake.
func (s *Session) HandleRx(now time.Time, ctrl *ControlPacket) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == StateAdminDown {
		return false
	}
	if ctrl.peerDiscrr != 0 && ctrl.peerDiscrr != s.localDiscr {
		return false
	}

	prev := s.state

	// Learn peer discriminator if not yet known.
	if s.peerDiscr == 0 && ctrl.LocalDiscrr != 0 {
		s.peerDiscr = ctrl.LocalDiscrr
	}

	// Update peer timing and clamp within floor/ceiling bounds.
	rtx := time.Duration(ctrl.DesiredMinTxUs) * time.Microsecond
	rrx := time.Duration(ctrl.RequiredMinRxUs) * time.Microsecond
	if rtx < s.minTxFloor {
		rtx = s.minTxFloor
	} else if s.maxTxCeil > 0 && rtx > s.maxTxCeil {
		rtx = s.maxTxCeil
	}
	if rrx < s.minTxFloor {
		rrx = s.minTxFloor
	} else if s.maxTxCeil > 0 && rrx > s.maxTxCeil {
		rrx = s.maxTxCeil
	}
	s.peerTxMin, s.peerRxMin = rtx, rrx
	s.lastRx = now
	s.detectDeadline = now.Add(s.detectTime())

	switch prev {
	case StateDown:
		// Mark convergence-to-up start at the first successful RX while Down.
		if s.convUpStart.IsZero() {
			s.convUpStart = now
		}

		// Move to Init once peer identified; Up after echo confirmation.
		if s.peerDiscr != 0 {
			if ctrl.State >= StateInit && ctrl.peerDiscrr == s.localDiscr {
				s.state = StateUp
				s.backoffFactor = 1
			} else {
				s.state = StateInit
				s.backoffFactor = 1
			}
		}

	case StateInit:
		// Promote to Up only after receiving echo referencing our localDiscr.
		if s.peerDiscr != 0 && ctrl.State >= StateInit && ctrl.peerDiscrr == s.localDiscr {
			s.state = StateUp
			s.backoffFactor = 1
		}

	case StateUp:
		// If peer advertises Down, immediately mirror it and pause detect.
		if ctrl.State == StateDown {
			if s.convDownStart.IsZero() {
				// Start convergence-to-down at this RX moment if not already set.
				s.convDownStart = now
			}
			s.state = StateDown
			s.backoffFactor = 1
			s.detectDeadline = time.Time{}
		}
	}

	return s.state != prev
}

// detectTime computes detection interval as detectMult × rxInterval().
func (s *Session) detectTime() time.Duration {
	return time.Duration(int64(s.detectMult) * int64(s.rxInterval()))
}

// txInterval picks the effective transmit interval, bounded by floors/ceilings,
// using the greater of localTxMin and peerRxMin.
func (s *Session) txInterval() time.Duration {
	iv := s.localTxMin
	if s.peerRxMin > iv {
		iv = s.peerRxMin
	}
	if iv < s.minTxFloor {
		iv = s.minTxFloor
	}
	if iv > s.maxTxCeil {
		iv = s.maxTxCeil
	}
	return iv
}

// rxInterval picks the effective receive interval based on peer TX and
// our own desired RX, clamped to the same bounds.
func (s *Session) rxInterval() time.Duration {
	ref := s.peerTxMin
	if s.localRxMin > ref {
		ref = s.localRxMin
	}
	if ref == 0 {
		ref = s.localRxMin
	}
	if ref < s.minTxFloor {
		ref = s.minTxFloor
	}
	if ref > s.maxTxCeil {
		ref = s.maxTxCeil
	}
	return ref
}
