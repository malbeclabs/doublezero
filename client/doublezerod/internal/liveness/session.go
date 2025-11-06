package liveness

import (
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type Session struct {
	route *routing.Route

	myDisc   uint32
	yourDisc uint32
	state    State

	detectMult               uint8
	localTxMin, localRxMin   time.Duration
	remoteTxMin, remoteRxMin time.Duration

	nextTx, detectDeadline, lastRx time.Time

	peer     *Peer
	peerAddr *net.UDPAddr

	alive bool

	minTxFloor, maxTxCeil time.Duration

	mu sync.Mutex
}

// Compute jittered next TX time and persist it into s.nextTx.
// Returns the chosen time.
func (s *Session) ComputeNextTx(now time.Time, rnd *rand.Rand) time.Time {
	s.mu.Lock()
	iv := s.txInterval()
	j := iv / 10
	var r int
	if rnd != nil {
		r = rnd.Intn(int(2*j + 1))
	} else {
		r = rand.Intn(int(2*j + 1))
	}
	jit := time.Duration(r) - j
	next := now.Add(iv + jit)
	s.nextTx = next
	s.mu.Unlock()
	return next
}

// Ensure detect is armed and not stale; updates detectDeadline if needed.
// Returns (deadline, true) if detect should be (re)scheduled, false if not.
func (s *Session) ArmDetect(now time.Time) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.alive {
		return time.Time{}, false
	}
	if s.detectDeadline.IsZero() {
		return time.Time{}, false
	}
	ddl := s.detectDeadline
	if !ddl.After(now) {
		ddl = now.Add(s.detectTime())
		s.detectDeadline = ddl
	}
	return ddl, true
}

// ExpireIfDue checks whether the session’s detect deadline has elapsed and,
// if so, transitions it to Down and clears the deadline. It returns true
// if the state changed. Callers are responsible for scheduling follow-up
// actions (e.g. notifying or rescheduling) based on the result.
func (s *Session) ExpireIfDue(now time.Time) (expired bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.alive {
		return false
	}

	if (s.state == StateUp || s.state == StateInit) &&
		!s.detectDeadline.IsZero() &&
		!now.Before(s.detectDeadline) {
		s.state = StateDown
		s.detectDeadline = time.Time{} // stop detect while Down
		return true
	}
	return false
}

// HandleRx processes an incoming control packet and updates the session state.
// It validates the discriminator, refreshes remote timing parameters, and resets
// the detection deadline. Based on the peer’s advertised state, it transitions
// between Down, Init, and Up according to the BFD state machine rules. It returns
// true if the local session state changed as a result.
func (s *Session) HandleRx(now time.Time, ctrl *ControlPacket) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ignore all RX while locally AdminDown (operator-forced inactivity).
	if s.state == StateAdminDown {
		return false
	}

	// Ignore if peer explicitly targets a different session.
	if ctrl.YourDiscr != 0 && ctrl.YourDiscr != s.myDisc {
		return false
	}

	prev := s.state

	// Learn/refresh peer discriminator.
	if s.yourDisc == 0 && ctrl.MyDiscr != 0 {
		s.yourDisc = ctrl.MyDiscr
	}

	// Peer timers + (re)arm detect on any valid RX.
	s.remoteTxMin = time.Duration(ctrl.DesiredMinTxUs) * time.Microsecond
	s.remoteRxMin = time.Duration(ctrl.RequiredMinRxUs) * time.Microsecond
	s.lastRx = now
	s.detectDeadline = now.Add(s.detectTime())

	switch prev {
	case StateDown:
		// Bring-up: as soon as we can identify the peer, move to Init.
		// If the peer already reports >= Init, go straight to Up.
		if s.yourDisc != 0 {
			if ctrl.State >= StateInit {
				// If peer is reporting Init or Up, promote our session to Up
				// Confirmation Phase: State = Up
				s.state = StateUp
			} else {
				// If peer is reporting Down, promote our session to Init
				// Learning Phase: State = Init
				s.state = StateInit
			}
		}

	case StateInit:
		// Do NOT mirror Down while initializing; let detect expiry handle failure.
		// Promote to Up once the peer reports >= Init.
		if s.yourDisc != 0 && ctrl.State >= StateInit {
			// If peer is reporting Init or Up, promote our session to Up
			// Confirmation Phase: State = Up
			s.state = StateUp
		}

	case StateUp:
		// Established and peer declares Down -> mirror once and stop detect.
		if ctrl.State == StateDown {
			// If peer is reporting Down, degrade our session to Down
			// De-activation Phase: State = Down
			s.state = StateDown
			s.detectDeadline = time.Time{} // stop detect while Down
		}
	}

	return s.state != prev
}

func (s *Session) detectTime() time.Duration {
	return time.Duration(int64(s.detectMult) * int64(s.rxRef()))
}

func (s *Session) txInterval() time.Duration {
	iv := s.localTxMin
	if s.remoteRxMin > iv {
		iv = s.remoteRxMin
	}
	if iv < s.minTxFloor {
		iv = s.minTxFloor
	}
	if iv > s.maxTxCeil {
		iv = s.maxTxCeil
	}
	return iv
}

func (s *Session) rxRef() time.Duration {
	ref := s.remoteTxMin
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
