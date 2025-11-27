package liveness

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type DownReason uint8

const (
	DownReasonNone DownReason = iota
	DownReasonTimeout
	DownReasonRemoteAdmin
	DownReasonLocalAdmin
	DownReasonRemoteDown
)

func (d DownReason) String() string {
	switch d {
	case DownReasonNone:
		return "none"
	case DownReasonTimeout:
		return "timeout"
	case DownReasonRemoteAdmin:
		return "remote_admin"
	case DownReasonLocalAdmin:
		return "local_admin"
	case DownReasonRemoteDown:
		return "remote_down"
	}
	return fmt.Sprintf("unknown(%d)", d)
}

type KernelState uint8

const (
	KernelStateUnknown KernelState = iota
	KernelStateAbsent
	KernelStatePresent
)

func (s KernelState) String() string {
	switch s {
	case KernelStateUnknown:
		return "unknown"
	case KernelStateAbsent:
		return "absent"
	case KernelStatePresent:
		return "present"
	}
	return fmt.Sprintf("unknown(%d)", s)
}

type PeerMode uint8

const (
	PeerModeUnknown PeerMode = iota
	PeerModeActive
	PeerModePassive
)

func (p PeerMode) String() string {
	switch p {
	case PeerModeUnknown:
		return "unknown"
	case PeerModeActive:
		return "active"
	case PeerModePassive:
		return "passive"
	}
	return fmt.Sprintf("unknown(%d)", p)
}

// Session models a single bidirectional liveness relationship with a peer,
// maintaining BFD-like state, timers, and randomized transmission scheduling.
type Session struct {
	route *routing.Route

	localDiscr, peerDiscr uint32 // discriminators identify this session to each side
	state                 State  // current BFD state

	upSince        time.Time  // time we transitioned to Up
	downSince      time.Time  // time we transitioned to Down
	lastDownReason DownReason // reason for last transition to Down
	lastUpdated    time.Time  // time we last updated the session

	peerAdvertisedMode PeerMode      // peer advertised mode
	peerClientVersion  ClientVersion // peer advertised client version

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
		s.downSince = now
		s.lastDownReason = DownReasonTimeout
		s.upSince = time.Time{}
		s.backoffFactor = 1
		s.detectDeadline = time.Time{}
		s.convUpStart = time.Time{}
		s.lastUpdated = now
		return true
	}
	return false
}

// HandleRx processes one control packet.
//
// - Ignore all packets while in AdminDown.
// - Drop if PeerDiscr is non-zero and not equal to our localDiscr.
// - If ctrl.State==AdminDown: transition → Down, clear detect, reset backoff.
// - If ctrl.State==Down while we are Up/Init: transition → Down (peer signaled failure).
// - Learn peerDiscr if unset. Update peer timers. Refresh detectDeadline.
// - Down: if peerDiscr known and peer echoes our localDiscr with State≥Init → Up; else → Init.
// - Init: promote to Up when peer echoes our localDiscr with State≥Init.
// - Up: refresh detect; allow LocalDiscr changes (peer restart).
// - Timeout handling occurs in ExpireIfDue().
func (s *Session) HandleRx(now time.Time, ctrl *ControlPacket) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Local AdminDown: ignore all incoming control packets.
	if s.state == StateAdminDown {
		return false
	}

	// Reject packets that claim some other session (wrong PeerDiscr).
	if ctrl.PeerDiscr != 0 && ctrl.PeerDiscr != s.localDiscr {
		return false
	}

	// Learn mode advertised by the peer.
	if ctrl.IsPassive() {
		s.peerAdvertisedMode = PeerModePassive
	} else {
		s.peerAdvertisedMode = PeerModeActive
	}

	// Learn client version advertised by the peer.
	s.peerClientVersion = ctrl.ClientVersion

	prev := s.state

	// If peer is in AdminDown, treat this as an intentional shutdown.
	if ctrl.State == StateAdminDown {
		if (prev == StateUp || prev == StateInit) && s.convDownStart.IsZero() {
			s.convDownStart = now
		}
		s.state = StateDown
		s.downSince = now
		s.lastDownReason = DownReasonRemoteAdmin
		s.upSince = time.Time{}
		s.detectDeadline = time.Time{}
		s.backoffFactor = 1
		s.lastUpdated = now
		return s.state != prev
	}

	// If peer says Down while we are Up/Init, treat as failure,
	// but suppress stale Down if we only just came Up (re-establishment race).
	if ctrl.State == StateDown && (prev == StateUp || prev == StateInit) {
		if prev == StateUp && !s.upSince.IsZero() {
			age := now.Sub(s.upSince)
			dt := s.detectTime() // detectMult × rxInterval
			if dt > 0 && age < dt {
				// Ignore stale remote_down that leaked from the peer's
				// detect timeout during re-establishment.
				return false
			}
		}

		if s.convDownStart.IsZero() {
			s.convDownStart = now
		}
		s.state = StateDown
		s.downSince = now
		s.lastDownReason = DownReasonRemoteDown
		s.upSince = time.Time{}
		s.detectDeadline = time.Time{}
		s.backoffFactor = 1
		s.convUpStart = time.Time{}
		s.lastUpdated = now
		return s.state != prev
	}

	// Learn peer discriminator if not yet known.
	if s.peerDiscr == 0 && ctrl.LocalDiscr != 0 {
		s.peerDiscr = ctrl.LocalDiscr
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
		if s.convUpStart.IsZero() {
			s.convUpStart = now
		}
		if s.peerDiscr != 0 {
			if ctrl.PeerDiscr == s.localDiscr && ctrl.State >= StateInit {
				s.state = StateUp
				s.upSince = now
				s.downSince = time.Time{}
				s.lastDownReason = DownReasonNone
			} else {
				s.state = StateInit
				s.downSince = time.Time{}
				s.lastDownReason = DownReasonNone
			}
			s.backoffFactor = 1
			s.lastUpdated = now
		}

	case StateInit:
		if s.peerDiscr != 0 &&
			ctrl.State >= StateInit &&
			ctrl.PeerDiscr == s.localDiscr {
			s.state = StateUp
			s.upSince = now
			s.downSince = time.Time{}
			s.lastDownReason = DownReasonNone
			s.backoffFactor = 1
			s.lastUpdated = now
		}

	case StateUp:
		// Handle peer restart: new LocalDiscr, but keep session Up
		// as long as packets keep flowing; timeout will detect real failure.
		if ctrl.LocalDiscr != 0 && ctrl.LocalDiscr != s.peerDiscr {
			s.peerDiscr = ctrl.LocalDiscr
			s.convUpStart = now
			s.lastUpdated = now
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

type SessionSnapshot struct {
	Peer                Peer
	Route               routing.Route
	State               State
	LocalDiscr          uint32
	PeerDiscr           uint32
	ConvUpStart         time.Time
	ConvDownStart       time.Time
	UpSince             time.Time
	DownSince           time.Time
	LastDownReason      DownReason
	DetectDeadline      time.Time
	NextDetectScheduled time.Time
	LastUpdated         time.Time
	PeerAdvertisedMode  PeerMode
	PeerClientVersion   ClientVersion
	ExpectedKernelState KernelState
}

func (s *Session) Snapshot() SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	var peer Peer
	if s.peer != nil {
		peer = *s.peer
	}
	var route routing.Route
	if s.route != nil {
		route = *s.route
	}
	return SessionSnapshot{
		Peer:                peer,
		Route:               route,
		State:               s.state,
		LocalDiscr:          s.localDiscr,
		PeerDiscr:           s.peerDiscr,
		ConvUpStart:         s.convUpStart,
		ConvDownStart:       s.convDownStart,
		UpSince:             s.upSince,
		DownSince:           s.downSince,
		LastDownReason:      s.lastDownReason,
		DetectDeadline:      s.detectDeadline,
		NextDetectScheduled: s.nextDetectScheduled,
		LastUpdated:         s.lastUpdated,
		PeerAdvertisedMode:  s.peerAdvertisedMode,
		PeerClientVersion:   s.peerClientVersion,
	}
}
