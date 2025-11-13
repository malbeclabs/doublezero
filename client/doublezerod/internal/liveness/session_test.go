package liveness

import (
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newSess() *Session {
	return &Session{
		route:          nil,
		localDiscr:     0xAABBCCDD,
		peerDiscr:      0,
		state:          StateDown,
		detectMult:     3,
		localTxMin:     20 * time.Millisecond,
		localRxMin:     15 * time.Millisecond,
		peerTxMin:      10 * time.Millisecond,
		peerRxMin:      0,
		minTxFloor:     5 * time.Millisecond,
		maxTxCeil:      10 * time.Second,
		alive:          true,
		peer:           nil,
		peerAddr:       &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999},
		nextTx:         time.Time{},
		detectDeadline: time.Time{},
		lastRx:         time.Time{},
		backoffMax:     1 * time.Second,
		backoffFactor:  1,
	}
}

func TestClient_Liveness_Session_ComputeNextTx_JitterWithinBoundsAndPersists(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.localTxMin = 100 * time.Millisecond
	s.state = StateDown
	now := time.Unix(0, 0)
	r := rand.New(rand.NewSource(1))
	next := s.ComputeNextTx(now, r)

	base := s.txInterval()
	j := base / 10
	min := now.Add(base - j)
	max := now.Add(base + j)

	require.True(t, !next.Before(min) && !next.After(max), "next=%v min=%v max=%v", next, min, max)
	require.Equal(t, next, s.nextTx)
	require.Equal(t, uint32(2), s.backoffFactor, "backoff should double after scheduling while Down")
}

func TestClient_Liveness_Session_TxIntervalRespectspeerRxMinFloorAndCeil(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.localTxMin = 20 * time.Millisecond
	s.peerRxMin = 50 * time.Millisecond
	s.minTxFloor = 60 * time.Millisecond
	s.maxTxCeil = 40 * time.Millisecond
	require.Equal(t, 40*time.Millisecond, s.txInterval())
}

func TestClient_Liveness_Session_RxRefPrefersMaxFloorAndCeil(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.peerTxMin = 10 * time.Millisecond
	s.localRxMin = 20 * time.Millisecond
	s.minTxFloor = 5 * time.Millisecond
	require.Equal(t, 20*time.Millisecond, s.rxInterval())

	s.peerTxMin = 0
	s.localRxMin = 0
	s.minTxFloor = 7 * time.Millisecond
	require.Equal(t, 7*time.Millisecond, s.rxInterval())

	// ceiling: cap overly large refs
	s.peerTxMin = 5 * time.Second
	s.localRxMin = 10 * time.Second
	s.minTxFloor = 1 * time.Millisecond
	s.maxTxCeil = 500 * time.Millisecond
	require.Equal(t, 500*time.Millisecond, s.rxInterval())
}

func TestClient_Liveness_Session_DetectTimeIsDetectMultTimesRxRef(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.detectMult = 5
	s.peerTxMin = 11 * time.Millisecond
	s.localRxMin = 13 * time.Millisecond // max with peerTxMin => 13ms
	s.minTxFloor = 3 * time.Millisecond
	require.Equal(t, 5*13*time.Millisecond, s.detectTime())
}

func TestClient_Liveness_Session_ArmDetectNotAliveOrZeroDeadlineReturnsFalse(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.alive = false
	s.detectDeadline = time.Now().Add(1 * time.Second)
	_, ok := s.ArmDetect(time.Now())
	require.False(t, ok)

	s = newSess()
	s.alive = true
	s.detectDeadline = time.Time{}
	_, ok = s.ArmDetect(time.Now())
	require.False(t, ok)
}

func TestClient_Liveness_Session_ArmDetectFutureDeadlineReturnsSameTrue(t *testing.T) {
	t.Parallel()
	s := newSess()
	now := time.Now()
	want := now.Add(500 * time.Millisecond)
	s.detectDeadline = want
	ddl, ok := s.ArmDetect(now)
	require.True(t, ok)
	require.Equal(t, want, ddl)
	require.Equal(t, want, s.detectDeadline)
}

func TestClient_Liveness_Session_ArmDetectPastDeadlineReschedules(t *testing.T) {
	t.Parallel()
	s := newSess()
	now := time.Now()
	s.detectDeadline = now.Add(-1 * time.Millisecond)
	ddl, ok := s.ArmDetect(now)
	require.True(t, ok)
	require.True(t, ddl.After(now))
	require.Equal(t, ddl, s.detectDeadline)
}

func TestClient_Liveness_Session_ExpireIfDueTransitionsToDownAndClearsDeadline(t *testing.T) {
	t.Parallel()
	s := newSess()
	now := time.Now()
	s.state = StateUp
	s.detectDeadline = now.Add(-1 * time.Millisecond)
	exp := s.ExpireIfDue(now)
	require.True(t, exp)
	require.Equal(t, StateDown, s.state)
	require.True(t, s.detectDeadline.IsZero())
	require.Equal(t, uint32(1), s.backoffFactor, "backoff should reset after transition to Down")
}

func TestClient_Liveness_Session_ExpireIfDueNoTransitionWhenNotDueOrNotAlive(t *testing.T) {
	t.Parallel()
	s := newSess()
	now := time.Now()
	s.state = StateInit
	s.detectDeadline = now.Add(1 * time.Second)
	require.False(t, s.ExpireIfDue(now))
	require.Equal(t, StateInit, s.state)

	s = newSess()
	s.state = StateUp
	s.alive = false
	s.detectDeadline = now.Add(-1 * time.Millisecond)
	require.False(t, s.ExpireIfDue(now))
	require.Equal(t, StateUp, s.state)
}

func TestClient_Liveness_Session_HandleRxIgnoresMismatchedpeerDiscrr(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.localDiscr = 111
	now := time.Now()
	cp := &ControlPacket{PeerDiscr: 222, LocalDiscr: 333, State: StateInit}
	changed := s.HandleRx(now, cp)
	require.False(t, changed)
	require.Equal(t, StateDown, s.state)
	require.Zero(t, s.peerDiscr)
}

func TestClient_Liveness_Session_HandleRxFromDownToInitOrUpAndArmsDetect(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.state = StateDown
	s.localDiscr = 42

	now := time.Now()
	// Peer Down -> go Init
	cpDown := &ControlPacket{
		PeerDiscr:       0,    // acceptable (we only check mismatch if nonzero)
		LocalDiscr:      1001, // learn peer discr
		State:           StateDown,
		DesiredMinTxUs:  30_000, // 30ms
		RequiredMinRxUs: 40_000, // 40ms
	}
	changed := s.HandleRx(now, cpDown)
	require.True(t, changed)
	require.Equal(t, StateInit, s.state)
	require.EqualValues(t, 1001, s.peerDiscr)
	require.False(t, s.detectDeadline.IsZero())
	require.Equal(t, now, s.lastRx)

	// Next packet peer Init -> go Up
	cpInit := &ControlPacket{
		PeerDiscr:       42, // matches our localDiscr (explicit echo required)
		LocalDiscr:      1001,
		State:           StateInit,
		DesiredMinTxUs:  20_000,
		RequiredMinRxUs: 20_000,
	}
	changed = s.HandleRx(now.Add(10*time.Millisecond), cpInit)
	require.True(t, changed)
	require.Equal(t, StateUp, s.state)
	require.Equal(t, uint32(1), s.backoffFactor, "backoff should reset when leaving Down")
}

func TestClient_Liveness_Session_HandleRxFromInitToUpOnPeerInitOrUp(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.state = StateInit
	s.peerDiscr = 777 // already learned
	now := time.Now()

	// Without explicit echo (PeerDiscr != localDiscr), do NOT promote.
	cpNoEcho := &ControlPacket{PeerDiscr: 0, LocalDiscr: 777, State: StateUp}
	changed := s.HandleRx(now, cpNoEcho)
	require.False(t, changed)
	require.Equal(t, StateInit, s.state)

	// With explicit echo (PeerDiscr == localDiscr), promote to Up.
	cpEcho := &ControlPacket{PeerDiscr: s.localDiscr, LocalDiscr: s.peerDiscr, State: StateUp}
	changed = s.HandleRx(now, cpEcho)
	require.True(t, changed)
	require.Equal(t, StateUp, s.state)
}

func TestClient_Liveness_Session_HandleRxFromUpToDownWhenPeerReportsDownAndStopDetect(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.state = StateUp
	s.peerDiscr = 1
	now := time.Now()
	s.detectDeadline = now.Add(10 * time.Second)

	cp := &ControlPacket{PeerDiscr: 0, LocalDiscr: 1, State: StateDown}
	changed := s.HandleRx(now, cp)
	require.True(t, changed)
	require.Equal(t, StateDown, s.state)
	require.True(t, s.detectDeadline.IsZero())
	require.Equal(t, uint32(1), s.backoffFactor, "backoff should reset when entering Down")
}

func TestClient_Liveness_Session_HandleRxSetsPeerTimersAndDetectDeadline(t *testing.T) {
	t.Parallel()
	s := newSess()
	now := time.Now()
	cp := &ControlPacket{
		PeerDiscr:       0,
		LocalDiscr:      9,
		State:           StateInit,
		DesiredMinTxUs:  12_000,
		RequiredMinRxUs: 34_000,
	}
	_ = s.HandleRx(now, cp)
	require.Equal(t, 12*time.Millisecond, s.peerTxMin)
	require.Equal(t, 34*time.Millisecond, s.peerRxMin)
	require.False(t, s.detectDeadline.IsZero())
	require.Equal(t, now, s.lastRx)
}

func TestClient_Liveness_Session_BackoffResetsWhenNotDown(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.state = StateDown
	s.backoffFactor = 8
	s.backoffMax = 200 * time.Millisecond
	_ = s.ComputeNextTx(time.Now(), nil) // will keep doubling (capped) while Down
	s.state = StateUp
	_ = s.ComputeNextTx(time.Now(), nil) // leaves Down -> resets
	require.Equal(t, uint32(1), s.backoffFactor)
}

func TestClient_Liveness_Session_HandleRxIgnoredWhenAdminDown(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.state = StateAdminDown
	now := time.Now()
	cp := &ControlPacket{PeerDiscr: 0, LocalDiscr: 9, State: StateUp, DesiredMinTxUs: 1000, RequiredMinRxUs: 2000}
	changed := s.HandleRx(now, cp)
	require.False(t, changed)
	require.Equal(t, StateAdminDown, s.state)
	require.Zero(t, s.peerDiscr)
}

func TestClient_Liveness_Session_HandleRxClampsTimersAndDetectMultZero(t *testing.T) {
	t.Parallel()
	s := newSess()
	now := time.Now()
	// Configure floors/ceils to make clamping observable.
	s.minTxFloor = 7 * time.Millisecond
	s.maxTxCeil = 40 * time.Millisecond

	cp := &ControlPacket{
		PeerDiscr:       0,
		LocalDiscr:      9,
		State:           StateInit,
		DetectMult:      0,         // invalid → clamp to 1 (internal)
		DesiredMinTxUs:  1_000,     // 1ms  → clamp up to 7ms
		RequiredMinRxUs: 1_000_000, // 1s   → clamp down to 40ms
	}
	_ = s.HandleRx(now, cp)

	require.Equal(t, 7*time.Millisecond, s.peerTxMin)
	require.Equal(t, 40*time.Millisecond, s.peerRxMin)
	require.False(t, s.detectDeadline.IsZero())
}

func TestClient_Liveness_Session_ComputeNextTx_LargeInterval_NoOverflow(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.localTxMin = 3 * time.Hour
	s.state = StateUp
	require.NotPanics(t, func() { _ = s.ComputeNextTx(time.Now(), rand.New(rand.NewSource(1))) })
}

func TestClient_Liveness_Session_HandleRx_NoChange_RearmsDetect(t *testing.T) {
	t.Parallel()
	s := newSess()
	now := time.Now()
	s.state = StateUp
	s.detectDeadline = now.Add(100 * time.Millisecond)

	callNow := now.Add(10 * time.Millisecond)
	cp := &ControlPacket{
		PeerDiscr:       s.localDiscr, // accepted (echo ok)
		LocalDiscr:      s.peerDiscr,  // may be 0; fine
		State:           StateUp,
		DesiredMinTxUs:  20000, // 20ms
		RequiredMinRxUs: 20000,
	}
	changed := s.HandleRx(callNow, cp)
	require.False(t, changed)

	// Expect re-armed to ≈ callNow + detectTime()
	wantMin := callNow.Add(s.detectTime() - 2*time.Millisecond) // tiny slack
	wantMax := callNow.Add(s.detectTime() + 2*time.Millisecond)
	require.True(t, !s.detectDeadline.Before(wantMin) && !s.detectDeadline.After(wantMax),
		"got=%v want≈%v", s.detectDeadline, callNow.Add(s.detectTime()))
}
