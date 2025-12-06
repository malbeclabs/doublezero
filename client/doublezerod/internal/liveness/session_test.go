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
	require.Equal(t, DownReasonTimeout, s.lastDownReason)
	require.False(t, s.downSince.IsZero())
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

	// First packet: peer advertises INIT and has a discriminator.
	// We should learn peerDiscr, go INIT, and arm detect.
	cpInit1 := &ControlPacket{
		Version:         1,
		State:           StateInit,
		DetectMult:      3,
		Length:          40,
		LocalDiscr:      1001, // peer's discr
		PeerDiscr:       0,
		DesiredMinTxUs:  30_000, // 30ms
		RequiredMinRxUs: 40_000, // 40ms
	}

	changed := s.HandleRx(now, cpInit1)
	require.True(t, changed)
	require.Equal(t, StateInit, s.state)
	require.EqualValues(t, 1001, s.peerDiscr)
	require.False(t, s.detectDeadline.IsZero())
	require.Equal(t, now, s.lastRx)

	// Second packet: peer INIT and echoing our localDiscr → go UP.
	cpInit2 := &ControlPacket{
		Version:         1,
		State:           StateInit,
		DetectMult:      3,
		Length:          40,
		LocalDiscr:      1001, // same peer discr
		PeerDiscr:       42,   // echo our localDiscr
		DesiredMinTxUs:  20_000,
		RequiredMinRxUs: 20_000,
	}

	changed = s.HandleRx(now.Add(10*time.Millisecond), cpInit2)
	require.True(t, changed)
	require.Equal(t, StateUp, s.state)
	require.Equal(t, uint32(1), s.backoffFactor, "backoff should reset when leaving Down/Init")
	require.True(t, s.upSince.After(time.Time{}))
	require.Equal(t, DownReasonNone, s.lastDownReason)
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
	require.Equal(t, DownReasonNone, s.lastDownReason)
}

func TestClient_Liveness_Session_HandleRxFromUpToDownWhenPeerReportsAdminDown(t *testing.T) {
	t.Parallel()
	s := newSess()
	s.state = StateUp
	s.peerDiscr = 1
	now := time.Now()
	s.detectDeadline = now.Add(10 * time.Second)

	// Peer goes AdminDown; we treat this as intentional remote disable → Down.
	cp := &ControlPacket{PeerDiscr: 0, LocalDiscr: 1, State: StateAdminDown}
	changed := s.HandleRx(now, cp)
	require.True(t, changed)
	require.Equal(t, StateDown, s.state)
	require.True(t, s.detectDeadline.IsZero())
	require.Equal(t, uint32(1), s.backoffFactor, "backoff should reset when entering Down")
	require.Equal(t, DownReasonRemoteAdmin, s.lastDownReason)
	require.False(t, s.downSince.IsZero())
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

	// Pre-populate some fields to ensure they don't change.
	s.peerDiscr = 0
	s.peerTxMin = 10 * time.Millisecond
	s.peerRxMin = 20 * time.Millisecond
	s.detectDeadline = time.Time{}
	s.lastRx = time.Time{}

	cp := &ControlPacket{
		PeerDiscr:       0,
		LocalDiscr:      9,
		State:           StateUp,
		DesiredMinTxUs:  1000,
		RequiredMinRxUs: 2000,
	}

	changed := s.HandleRx(now, cp)
	require.False(t, changed)
	require.Equal(t, StateAdminDown, s.state)
	require.Zero(t, s.peerDiscr)
	require.True(t, s.detectDeadline.IsZero())
	require.True(t, s.lastRx.IsZero())
}

func TestClient_Liveness_Session_HandleRxClampsPeerTimersAndArmsDetect(t *testing.T) {
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
		DetectMult:      0,         // ignored by HandleRx in current design
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

func TestClient_Liveness_Session_HandleRxFromUp_IgnoresPeerDownWithinDetectInterval(t *testing.T) {
	t.Parallel()

	s := newSess()
	now := time.Now()

	// Put session in Up, but "just" Up (UpFor < detectInterval).
	s.state = StateUp
	s.peerDiscr = 123
	s.upSince = now

	cp := &ControlPacket{
		PeerDiscr:       s.localDiscr, // echo our localDiscr so it's accepted
		LocalDiscr:      s.peerDiscr,  // peer's discr
		State:           StateDown,    // peer declares Down
		DesiredMinTxUs:  20_000,
		RequiredMinRxUs: 20_000,
	}

	callNow := now.Add(5 * time.Millisecond) // less than typical detectTime (3 * 15ms = 45ms in newSess)
	changed := s.HandleRx(callNow, cp)

	require.False(t, changed, "state should not change when UpFor < detect interval")
	require.Equal(t, StateUp, s.state, "session should remain Up")
	require.True(t, s.upSince.Equal(now), "upSince should not be reset on ignored Down")
}

func TestClient_Liveness_Session_HandleRxFromUpToDownWhenPeerReportsDownAfterDetectInterval(t *testing.T) {
	t.Parallel()

	s := newSess()
	now := time.Now()

	// Configure a stable Up long enough ago that UpFor >= detect interval.
	s.state = StateUp
	s.peerDiscr = 123
	detect := s.detectTime()
	s.upSince = now.Add(-2 * detect) // comfortably beyond one full detect interval

	cp := &ControlPacket{
		PeerDiscr:       s.localDiscr, // echo our localDiscr so it's accepted
		LocalDiscr:      s.peerDiscr,
		State:           StateDown,
		DesiredMinTxUs:  20_000,
		RequiredMinRxUs: 20_000,
	}

	changed := s.HandleRx(now, cp)

	require.True(t, changed, "state should change when UpFor >= detect interval and peer reports Down")
	require.Equal(t, StateDown, s.state)
	require.False(t, s.downSince.IsZero(), "downSince should be set on remote Down")
	require.True(t, s.detectDeadline.IsZero(), "detectDeadline should be cleared when transitioning to Down on remote Down")
	require.Equal(t, uint32(1), s.backoffFactor, "backoff should reset when entering Down")
}

func TestClient_Liveness_Session_HandleRx_TracksCurrentPeerAdvertisedPassive(t *testing.T) {
	t.Parallel()

	s := newSess()
	s.state = StateDown
	s.localDiscr = 42

	now := time.Now()

	cvPassive := ClientVersion{
		Major:   1,
		Minor:   2,
		Patch:   3,
		Channel: VersionChannelAlpha,
	}
	cvActive := ClientVersion{
		Major:   2,
		Minor:   0,
		Patch:   0,
		Channel: VersionChannelDev,
	}

	// First packet: peer advertises passive.
	cpPassive := &ControlPacket{
		Version:         1,
		State:           StateInit,
		DetectMult:      3,
		Length:          40,
		LocalDiscr:      1001,
		PeerDiscr:       0,
		DesiredMinTxUs:  30_000,
		RequiredMinRxUs: 40_000,
		ClientVersion:   cvPassive,
	}
	cpPassive.SetPassive()

	_ = s.HandleRx(now, cpPassive)
	require.Equal(t, PeerModePassive, s.peerAdvertisedMode, "should reflect current passive=on")

	snap := s.Snapshot()
	require.Equal(t, cvPassive, snap.PeerClientVersion, "snapshot should reflect peer's advertised client version (passive packet)")

	// Second packet: same session, but peer no longer advertises passive and changes version.
	cpActive := &ControlPacket{
		Version:         1,
		State:           StateInit,
		DetectMult:      3,
		Length:          40,
		LocalDiscr:      1001, // same peer discr
		PeerDiscr:       42,   // echo our localDiscr
		DesiredMinTxUs:  20_000,
		RequiredMinRxUs: 20_000,
		ClientVersion:   cvActive,
	}
	_ = s.HandleRx(now.Add(10*time.Millisecond), cpActive)

	require.Equal(t, PeerModeActive, s.peerAdvertisedMode, "peerAdvertisedMode should reflect current (no passive flag)")

	snap = s.Snapshot()
	require.Equal(t, cvActive, snap.PeerClientVersion, "snapshot should reflect latest advertised client version")
}
