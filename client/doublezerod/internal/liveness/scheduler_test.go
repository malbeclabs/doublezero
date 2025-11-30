package liveness

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClient_Liveness_Scheduler_EventQueueOrdering(t *testing.T) {
	t.Parallel()

	q := NewEventQueue()
	now := time.Now()
	e1 := &event{when: now}
	e2 := &event{when: now}
	e3 := &event{when: now.Add(5 * time.Millisecond)}

	q.Push(e1)
	q.Push(e2)
	q.Push(e3)

	// First PopIfDue returns first event immediately, zero wait
	ev, wait := q.PopIfDue(now)
	require.Equal(t, e1, ev)
	require.Zero(t, wait)

	// Second PopIfDue returns second event immediately, still zero wait
	ev, wait = q.PopIfDue(now)
	require.Equal(t, e2, ev)
	require.Zero(t, wait)

	// Third PopIfDue should not return yet, wait ~5ms
	ev, wait = q.PopIfDue(now)
	require.Nil(t, ev)
	require.InDelta(t, 5*time.Millisecond, wait, float64(time.Millisecond))
}

func TestClient_Liveness_Scheduler_TryExpireEnqueuesImmediateTX(t *testing.T) {
	t.Parallel()

	// minimal scheduler with a real EventQueue; udp/log not used here
	s := &Scheduler{log: newTestLogger(t), eq: NewEventQueue(), metrics: newMetrics()}
	sess := &Session{
		state:          StateUp,
		detectDeadline: time.Now().Add(-time.Millisecond),
		alive:          true,
		detectMult:     1,
		minTxFloor:     time.Millisecond,
		peer:           &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}
	ok := s.tryExpire(sess)
	require.True(t, ok)

	// first event should be immediate TX
	ev := s.eq.Pop()
	require.NotNil(t, ev)
	require.Equal(t, eventTypeTX, ev.eventType)

	// and state flipped to Down, detect cleared
	require.Equal(t, StateDown, sess.state)
	require.True(t, sess.detectDeadline.IsZero())
}

func TestClient_Liveness_Scheduler_ScheduleDetect_NoArmNoEnqueue(t *testing.T) {
	t.Parallel()
	s := &Scheduler{log: newTestLogger(t), eq: NewEventQueue(), metrics: newMetrics()}
	sess := &Session{alive: false} // ArmDetect will return false

	s.scheduleDetect(time.Now(), sess)
	require.Nil(t, s.eq.Pop()) // queue stays empty
}

func TestClient_Liveness_Scheduler_ScheduleDetect_EnqueuesDeadline(t *testing.T) {
	t.Parallel()
	s := &Scheduler{log: newTestLogger(t), eq: NewEventQueue(), metrics: newMetrics()}
	sess := &Session{
		alive:          true,
		detectDeadline: time.Now().Add(50 * time.Millisecond),
		detectMult:     1,
		minTxFloor:     time.Millisecond,
		peer:           &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}

	s.scheduleDetect(time.Now(), sess)
	ev := s.eq.Pop()
	require.NotNil(t, ev)
	require.Equal(t, eventTypeDetect, ev.eventType)
}

func TestClient_Liveness_Scheduler_TryExpire_Idempotent(t *testing.T) {
	t.Parallel()
	s := &Scheduler{log: newTestLogger(t), eq: NewEventQueue(), metrics: newMetrics()}
	sess := &Session{
		state:          StateUp,
		detectDeadline: time.Now().Add(-time.Millisecond),
		alive:          true,
		detectMult:     1,
		minTxFloor:     time.Millisecond,
		peer:           &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}
	require.True(t, s.tryExpire(sess))
	require.False(t, s.tryExpire(sess)) // second call no effect
}

func TestClient_Liveness_Scheduler_ScheduleTx_AdminDownAllowsSingleAdvert(t *testing.T) {
	t.Parallel()
	s := &Scheduler{log: newTestLogger(t), eq: NewEventQueue(), metrics: newMetrics()}
	sess := &Session{
		state:      StateAdminDown,
		alive:      true,
		detectMult: 1,
		minTxFloor: time.Millisecond,
		peer:       &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}

	// Explicit call (like AdminDownRoute) should enqueue a TX event.
	s.scheduleTx(time.Now(), sess)
	ev := s.eq.Pop()
	require.NotNil(t, ev, "AdminDown should still be able to schedule one TX advert")
	require.Equal(t, eventTypeTX, ev.eventType)
	require.Nil(t, s.eq.Pop(), "no additional TX should be queued by scheduleTx itself")
}

func TestClient_Liveness_Scheduler_ScheduleTx_AdaptiveBackoffWhenDown(t *testing.T) {
	t.Parallel()
	s := &Scheduler{log: newTestLogger(t), eq: NewEventQueue(), metrics: newMetrics()}
	sess := &Session{
		state:         StateDown,
		alive:         true,
		detectMult:    1,
		localTxMin:    20 * time.Millisecond,
		localRxMin:    20 * time.Millisecond,
		minTxFloor:    10 * time.Millisecond,
		maxTxCeil:     1 * time.Second,
		backoffMax:    150 * time.Millisecond,
		backoffFactor: 1,
		peer:          &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}

	now := time.Now()

	// First schedule: should enqueue a TX and bump backoffFactor in ComputeNextTx.
	s.scheduleTx(now, sess)
	ev1 := s.eq.Pop()
	require.NotNil(t, ev1)
	require.Equal(t, eventTypeTX, ev1.eventType)
	require.Greater(t, sess.backoffFactor, uint32(1)) // doubled to 2
	require.True(t, ev1.when.After(now))

	// Simulate Run loop clearing the pending TX marker when the event is consumed.
	sess.mu.Lock()
	if ev1.when.Equal(sess.nextTxScheduled) {
		sess.nextTxScheduled = time.Time{}
	}
	sess.mu.Unlock()

	// Second schedule: allowed now, should enqueue another TX and further backoff (up to cap).
	s.scheduleTx(now.Add(time.Millisecond), sess)
	ev2 := s.eq.Pop()
	require.NotNil(t, ev2)
	require.Equal(t, eventTypeTX, ev2.eventType)
	require.GreaterOrEqual(t, sess.backoffFactor, uint32(4))
	require.True(t, ev2.when.After(now))

	// Bound first interval by backoffMax (+ jitter slack)
	require.LessOrEqual(t, time.Until(ev1.when), time.Duration(float64(150*time.Millisecond)*1.5))
}

func TestClient_Liveness_Scheduler_Run_SendsAndReschedules(t *testing.T) {
	t.Parallel()
	// real UDP to count packets
	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer srv.Close()
	r, _ := NewUDPService(srv)
	cl, _ := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	defer cl.Close()
	w, _ := NewUDPService(cl)

	pkts := int32(0)
	stop := make(chan struct{})
	go func() {
		buf := make([]byte, 128)
		_ = srv.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			_, _, _, _, err := r.ReadFrom(buf)
			if err != nil {
				return
			}
			atomic.AddInt32(&pkts, 1)
		}
	}()

	log := newTestLogger(t)
	s := NewScheduler(log, w, func(*Session) {}, 0, false, newMetrics(), false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		require.NoError(t, s.Run(ctx))
	}()

	sess := &Session{
		state:         StateInit,
		alive:         true,
		localTxMin:    20 * time.Millisecond,
		localRxMin:    20 * time.Millisecond,
		minTxFloor:    10 * time.Millisecond,
		maxTxCeil:     200 * time.Millisecond,
		detectMult:    3,
		peer:          &Peer{Interface: "", LocalIP: cl.LocalAddr().(*net.UDPAddr).IP.String()},
		peerAddr:      srv.LocalAddr().(*net.UDPAddr),
		backoffMax:    200 * time.Millisecond,
		backoffFactor: 1,
	}
	s.scheduleTx(time.Now(), sess)
	time.Sleep(120 * time.Millisecond)
	cancel()
	close(stop)

	require.GreaterOrEqual(t, atomic.LoadInt32(&pkts), int32(2))
}

func TestClient_Liveness_Scheduler_ScheduleDetect_DedupSameDeadline(t *testing.T) {
	t.Parallel()

	s := &Scheduler{log: newTestLogger(t), eq: NewEventQueue(), metrics: newMetrics()}
	sess := &Session{
		alive:      true,
		detectMult: 1,
		minTxFloor: time.Millisecond,
		peer:       &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}

	// Use a fixed 'now' strictly before detectDeadline so ArmDetect does not re-arm.
	fixedNow := time.Now()
	sess.mu.Lock()
	sess.detectDeadline = fixedNow.Add(50 * time.Millisecond)
	sess.mu.Unlock()

	// First enqueue for the deadline.
	s.scheduleDetect(fixedNow, sess)
	// Spam scheduleDetect with the SAME fixed 'now'; must not enqueue duplicates.
	for i := 0; i < 100; i++ {
		s.scheduleDetect(fixedNow, sess)
	}

	require.Equal(t, 1, s.eq.CountFor("eth0", "192.0.2.1"))

	ev := s.eq.Pop()
	require.NotNil(t, ev)
	require.Equal(t, eventTypeDetect, ev.eventType)
	require.Nil(t, s.eq.Pop())
}

func TestClient_Liveness_Scheduler_ScheduleDetect_AllowsNewDeadlineButStillDedupsPerDeadline(t *testing.T) {
	t.Parallel()

	s := &Scheduler{log: newTestLogger(t), eq: NewEventQueue(), metrics: newMetrics()}
	sess := &Session{
		alive:      true,
		detectMult: 1,
		minTxFloor: time.Millisecond,
		peer:       &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}

	base := time.Now()
	d1 := base.Add(40 * time.Millisecond)

	// Phase 1: schedule for D1 with fixed time < D1
	sess.mu.Lock()
	sess.detectDeadline = d1
	sess.mu.Unlock()
	s.scheduleDetect(base, sess)
	for range 10 {
		s.scheduleDetect(base, sess)
	}
	require.Equal(t, 1, s.eq.CountFor("eth0", "192.0.2.1"))

	// Phase 2: move to a new deadline D2; still call with fixed time < D2
	d2 := base.Add(90 * time.Millisecond)
	sess.mu.Lock()
	sess.detectDeadline = d2
	sess.mu.Unlock()
	for range 10 {
		s.scheduleDetect(base, sess)
	}

	// Exactly two detect events queued for this peer (D1 and D2)
	require.Equal(t, 2, s.eq.CountFor("eth0", "192.0.2.1"))

	// Pop order must be D1 then D2
	ev1 := s.eq.Pop()
	require.NotNil(t, ev1)
	require.Equal(t, eventTypeDetect, ev1.eventType)
	ev2 := s.eq.Pop()
	require.NotNil(t, ev2)
	require.Equal(t, eventTypeDetect, ev2.eventType)
	require.True(t, ev1.when.Before(ev2.when) || ev1.when.Equal(ev2.when))

	require.Nil(t, s.eq.Pop())
}

func TestClient_Liveness_Scheduler_ScheduleTx_DedupWhilePending(t *testing.T) {
	t.Parallel()

	s := &Scheduler{log: newTestLogger(t), eq: NewEventQueue(), metrics: newMetrics()}
	sess := &Session{
		state:         StateInit,
		alive:         true,
		localTxMin:    20 * time.Millisecond,
		localRxMin:    20 * time.Millisecond,
		minTxFloor:    10 * time.Millisecond,
		maxTxCeil:     200 * time.Millisecond,
		backoffMax:    200 * time.Millisecond,
		backoffFactor: 1,
		peer:          &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}

	// First schedule should enqueue exactly one TX.
	now := time.Now()
	s.scheduleTx(now, sess)

	// Repeated schedules while a TX is already pending must NOT enqueue more.
	for i := 0; i < 100; i++ {
		s.scheduleTx(now.Add(time.Duration(i)*time.Millisecond), sess)
	}

	require.Equal(t, 1, s.eq.CountFor("eth0", "192.0.2.1"))

	ev := s.eq.Pop()
	require.NotNil(t, ev)
	require.Equal(t, eventTypeTX, ev.eventType)
	require.Nil(t, s.eq.Pop())
}

func TestClient_Liveness_Scheduler_ScheduleTx_AllowsRescheduleAfterPop(t *testing.T) {
	t.Parallel()

	s := &Scheduler{log: newTestLogger(t), eq: NewEventQueue(), metrics: newMetrics()}
	sess := &Session{
		state:         StateInit,
		alive:         true,
		localTxMin:    20 * time.Millisecond,
		localRxMin:    20 * time.Millisecond,
		minTxFloor:    10 * time.Millisecond,
		maxTxCeil:     200 * time.Millisecond,
		backoffMax:    200 * time.Millisecond,
		backoffFactor: 1,
		peer:          &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}

	now := time.Now()
	s.scheduleTx(now, sess)
	ev := s.eq.Pop()
	require.NotNil(t, ev)
	require.Equal(t, eventTypeTX, ev.eventType)

	// Simulate the Run loop clearing the scheduled marker when the TX event is consumed.
	sess.mu.Lock()
	if ev.when.Equal(sess.nextTxScheduled) {
		sess.nextTxScheduled = time.Time{}
	}
	sess.mu.Unlock()

	// Now we should be able to schedule the next TX.
	s.scheduleTx(now.Add(5*time.Millisecond), sess)
	require.Equal(t, 1, s.eq.CountFor("eth0", "192.0.2.1"))

	ev2 := s.eq.Pop()
	require.NotNil(t, ev2)
	require.Equal(t, eventTypeTX, ev2.eventType)
	require.Nil(t, s.eq.Pop())
}

func TestClient_Liveness_Scheduler_ScheduleDetect_DropsOnOverflowAndClearsMarker(t *testing.T) {
	t.Parallel()

	s := &Scheduler{log: newTestLogger(t), eq: NewEventQueue(), maxEvents: 1, metrics: newMetrics()}
	sess := &Session{
		alive:      true,
		detectMult: 1,
		minTxFloor: time.Millisecond,
		peer:       &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}

	// Fill the queue to the cap with an unrelated event
	other := &Session{peer: &Peer{Interface: "ethX", LocalIP: "198.51.100.1"}}
	s.eq.Push(&event{when: time.Now().Add(time.Second), eventType: eventTypeTX, session: other})
	require.Equal(t, 1, s.eq.Len())

	// Try to schedule Detect; should be dropped due to overflow and marker cleared
	now := time.Now()
	sess.mu.Lock()
	sess.detectDeadline = now.Add(50 * time.Millisecond)
	sess.mu.Unlock()

	s.scheduleDetect(now, sess)

	require.Equal(t, 1, s.eq.Len(), "queue should remain at cap; detect dropped")
	sess.mu.Lock()
	require.True(t, sess.nextDetectScheduled.IsZero(), "dedupe marker must be cleared on drop")
	sess.mu.Unlock()
}

func TestClient_Liveness_Scheduler_Run_CullsStaleDetectAndClearsMarker(t *testing.T) {
	t.Parallel()

	log := newTestLogger(t)
	s := NewScheduler(log, nil, func(*Session) {}, 0, false, newMetrics(), false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	sess := &Session{
		alive:      true,
		detectMult: 1,
		minTxFloor: time.Millisecond,
		peer:       &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}

	// Make a stale detect: queued deadline d1, but current detectDeadline is d2.
	now := time.Now()
	d1 := now.Add(-1 * time.Millisecond) // already due -> scheduler will pop immediately
	d2 := now.Add(90 * time.Millisecond) // current detect deadline (different from d1)

	sess.mu.Lock()
	sess.detectDeadline = d2
	sess.nextDetectScheduled = d1 // simulate prior scheduling for d1
	sess.mu.Unlock()

	// Enqueue the stale detect event.
	s.eq.Push(&event{when: d1, eventType: eventTypeDetect, session: sess})
	require.Equal(t, 1, s.eq.Len())

	done := make(chan struct{})
	go func() { _ = s.Run(ctx); close(done) }()

	// Wait until the queue is empty (stale event culled) or time out
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if s.eq.Len() == 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	<-done

	require.Equal(t, 0, s.eq.Len(), "stale detect should be culled without rescheduling")

	sess.mu.Lock()
	require.True(t, sess.nextDetectScheduled.IsZero(), "marker must be cleared when stale event is dropped")
	require.Equal(t, d2, sess.detectDeadline, "current deadline must remain unchanged")
	sess.mu.Unlock()
}

func TestClient_Liveness_Scheduler_ScheduleTx_NotDroppedByOverflow(t *testing.T) {
	t.Parallel()

	s := &Scheduler{log: newTestLogger(t), eq: NewEventQueue(), maxEvents: 1, metrics: newMetrics()}
	sess := &Session{
		state:         StateInit,
		alive:         true,
		localTxMin:    20 * time.Millisecond,
		localRxMin:    20 * time.Millisecond,
		minTxFloor:    10 * time.Millisecond,
		maxTxCeil:     200 * time.Millisecond,
		backoffMax:    200 * time.Millisecond,
		backoffFactor: 1,
		peer:          &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}

	// Fill the queue to the cap with an unrelated event
	other := &Session{peer: &Peer{Interface: "ethX", LocalIP: "198.51.100.1"}}
	s.eq.Push(&event{when: time.Now().Add(time.Second), eventType: eventTypeDetect, session: other})
	require.Equal(t, 1, s.eq.Len())

	// scheduleTx should still enqueue despite overflow (policy: never drop TX)
	s.scheduleTx(time.Now(), sess)
	require.Equal(t, 2, s.eq.Len(), "TX must not be dropped by the soft cap")

	// Clean up: pop both; first could be either depending on 'when'
	require.NotNil(t, s.eq.Pop())
	require.NotNil(t, s.eq.Pop())
	require.Equal(t, 0, s.eq.Len())
}

func TestClient_Liveness_Scheduler_doTX_RespectsContextCancelOnWriteError(t *testing.T) {
	t.Parallel()

	srv, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 0,
	})
	require.NoError(t, err)
	defer srv.Close()

	// Client conn that we'll wrap in UDPService and then close to force WriteTo errors.
	cl, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 0,
	})
	require.NoError(t, err)

	udp, err := NewUDPService(cl)
	require.NoError(t, err)

	peerAddr := srv.LocalAddr().(*net.UDPAddr)
	localAddr := cl.LocalAddr().(*net.UDPAddr)

	// Force the underlying socket to be unusable for sending.
	require.NoError(t, cl.Close())

	// Sanity check: after Close, UDPService.WriteTo must fail; if this fails,
	// our assumptions about UDPService are wrong and the test should fail loudly.
	_, err = udp.WriteTo([]byte{0x01}, peerAddr, "", nil)
	require.Error(t, err, "expected UDPService.WriteTo to fail after closing underlying conn")

	var warns int32
	base := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})
	log := slog.New(&warnCountingHandler{inner: base, warnCount: &warns})

	s := NewScheduler(log, udp, func(*Session) {}, 0, false, newMetrics(), false)
	// Disable throttling so every error can emit a warn if allowed by ctx.
	s.writeErrWarnEvery = 0

	sess := &Session{
		state:         StateInit,
		alive:         true,
		localTxMin:    20 * time.Millisecond,
		localRxMin:    20 * time.Millisecond,
		minTxFloor:    10 * time.Millisecond,
		maxTxCeil:     200 * time.Millisecond,
		detectMult:    3,
		backoffMax:    200 * time.Millisecond,
		backoffFactor: 1,
		peer: &Peer{
			Interface: "",
			LocalIP:   localAddr.IP.String(),
			PeerIP:    peerAddr.IP.String(),
		},
		peerAddr: peerAddr,
	}

	// Live context: we EXPECT a warn on write error.
	ctxLive := context.Background()
	s.doTX(ctxLive, sess)
	require.GreaterOrEqual(t, atomic.LoadInt32(&warns), int32(1), "expected at least one warn with live context")

	// Reset warn count and throttling timestamp.
	atomic.StoreInt32(&warns, 0)
	s.writeErrWarnLast = time.Time{}

	// Canceled context: we EXPECT NO warn despite the write error.
	ctxCanceled, cancel := context.WithCancel(context.Background())
	cancel()

	s.doTX(ctxCanceled, sess)

	require.Equal(t, int32(0), atomic.LoadInt32(&warns), "no warn should be logged when ctx is already canceled")
}

func TestClient_Liveness_Scheduler_ScheduleTx_NoEnqueueWhenNotAlive(t *testing.T) {
	t.Parallel()

	s := &Scheduler{
		log:     newTestLogger(t),
		eq:      NewEventQueue(),
		metrics: newMetrics(),
	}

	sess := &Session{
		state:         StateInit,
		alive:         false,
		localTxMin:    20 * time.Millisecond,
		localRxMin:    20 * time.Millisecond,
		minTxFloor:    10 * time.Millisecond,
		maxTxCeil:     200 * time.Millisecond,
		backoffMax:    200 * time.Millisecond,
		backoffFactor: 1,
		peer:          &Peer{Interface: "eth0", LocalIP: "192.0.2.1"},
	}

	now := time.Now()

	// Not alive, non-AdminDown: no TX should be queued.
	s.scheduleTx(now, sess)
	require.Equal(t, 0, s.eq.Len(), "no TX should be scheduled when session is not alive")

	// Not alive, AdminDown: still no TX (AdminDown one-shot advert only applies when alive).
	sess.state = StateAdminDown
	s.scheduleTx(now.Add(5*time.Millisecond), sess)
	require.Equal(t, 0, s.eq.Len(), "no TX should be scheduled when session is not alive, even in AdminDown")
}

func TestClient_Liveness_Scheduler_doTX_SetsPassiveBitWhenPassiveModeEnabled(t *testing.T) {
	t.Parallel()

	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer srv.Close()
	r, _ := NewUDPService(srv)

	cl, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer cl.Close()
	w, _ := NewUDPService(cl)

	peerAddr := srv.LocalAddr().(*net.UDPAddr)

	// passiveMode=true here is the thing under test.
	s := NewScheduler(newTestLogger(t), w, func(*Session) {}, 0, false, newMetrics(), true)

	sess := &Session{
		state:         StateUp,
		alive:         true,
		localTxMin:    20 * time.Millisecond,
		localRxMin:    20 * time.Millisecond,
		minTxFloor:    10 * time.Millisecond,
		maxTxCeil:     200 * time.Millisecond,
		detectMult:    3,
		backoffMax:    200 * time.Millisecond,
		backoffFactor: 1,
		localDiscr:    123,
		peerDiscr:     456,
		peer: &Peer{
			Interface: "",
			LocalIP:   cl.LocalAddr().(*net.UDPAddr).IP.String(),
			PeerIP:    peerAddr.IP.String(),
		},
		peerAddr: peerAddr,
	}

	ctx := context.Background()
	s.doTX(ctx, sess)

	// Read one packet and decode it.
	buf := make([]byte, 128)
	_ = srv.SetReadDeadline(time.Now().Add(time.Second))
	n, _, _, _, err := r.ReadFrom(buf)
	require.NoError(t, err)

	cp, err := UnmarshalControlPacket(buf[:n])
	require.NoError(t, err)

	require.True(t, cp.IsPassive(), "expected Passive bit set when scheduler.passiveMode is true")
}

type warnCountingHandler struct {
	inner     slog.Handler
	warnCount *int32
}

func (h *warnCountingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *warnCountingHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		atomic.AddInt32(h.warnCount, 1)
	}
	return h.inner.Handle(ctx, r)
}

func (h *warnCountingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &warnCountingHandler{
		inner:     h.inner.WithAttrs(attrs),
		warnCount: h.warnCount,
	}
}

func (h *warnCountingHandler) WithGroup(name string) slog.Handler {
	return &warnCountingHandler{
		inner:     h.inner.WithGroup(name),
		warnCount: h.warnCount,
	}
}
