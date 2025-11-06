package liveness

import (
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

	// minimal scheduler with a real EventQueue; conn/log not used here
	s := &Scheduler{eq: NewEventQueue()}
	sess := &Session{
		state:          StateUp,
		detectDeadline: time.Now().Add(-time.Millisecond),
		alive:          true,
		detectMult:     1,
		minTxFloor:     time.Millisecond,
	}
	ok := s.tryExpire(sess)
	require.True(t, ok)

	// first event should be immediate TX
	ev := s.eq.Pop()
	require.NotNil(t, ev)
	require.Equal(t, evTX, ev.typ)

	// and state flipped to Down, detect cleared
	require.Equal(t, StateDown, sess.state)
	require.True(t, sess.detectDeadline.IsZero())
}

func TestClient_Liveness_Scheduler_ScheduleDetect_NoArmNoEnqueue(t *testing.T) {
	t.Parallel()
	s := &Scheduler{eq: NewEventQueue()}
	sess := &Session{alive: false} // ArmDetect will return false

	s.scheduleDetect(time.Now(), sess)
	require.Nil(t, s.eq.Pop()) // queue stays empty
}

func TestClient_Liveness_Scheduler_ScheduleDetect_EnqueuesDeadline(t *testing.T) {
	t.Parallel()
	s := &Scheduler{eq: NewEventQueue()}
	sess := &Session{
		alive:          true,
		detectDeadline: time.Now().Add(50 * time.Millisecond),
		detectMult:     1,
		minTxFloor:     time.Millisecond,
	}

	s.scheduleDetect(time.Now(), sess)
	ev := s.eq.Pop()
	require.NotNil(t, ev)
	require.Equal(t, evDetect, ev.typ)
}

func TestClient_Liveness_Scheduler_TryExpire_Idempotent(t *testing.T) {
	t.Parallel()
	s := &Scheduler{eq: NewEventQueue()}
	sess := &Session{
		state:          StateUp,
		detectDeadline: time.Now().Add(-time.Millisecond),
		alive:          true,
		detectMult:     1,
		minTxFloor:     time.Millisecond,
	}
	require.True(t, s.tryExpire(sess))
	require.False(t, s.tryExpire(sess)) // second call no effect
}

func TestClient_Liveness_Scheduler_ScheduleTx_NoEnqueueWhenAdminDown(t *testing.T) {
	t.Parallel()
	s := &Scheduler{eq: NewEventQueue()}
	sess := &Session{
		state:      StateAdminDown,
		alive:      true,
		detectMult: 1,
		minTxFloor: time.Millisecond,
	}
	s.scheduleTx(time.Now(), sess)
	require.Nil(t, s.eq.Pop(), "no TX should be scheduled while AdminDown")
}

func TestClient_Liveness_Scheduler_ScheduleTx_AdaptiveBackoffWhenDown(t *testing.T) {
	t.Parallel()
	s := &Scheduler{eq: NewEventQueue()}
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
	}
	now := time.Now()
	s.scheduleTx(now, sess)
	ev1 := s.eq.Pop()
	require.NotNil(t, ev1)
	require.Equal(t, evTX, ev1.typ)
	require.Greater(t, sess.backoffFactor, uint32(1)) // doubled to 2

	// next schedule should further increase backoff factor (up to ceil)
	s.scheduleTx(now.Add(time.Millisecond), sess)
	ev2 := s.eq.Pop()
	require.NotNil(t, ev2)
	require.Equal(t, evTX, ev2.typ)
	require.GreaterOrEqual(t, sess.backoffFactor, uint32(4))
	// both events should be scheduled in the future
	require.True(t, ev1.when.After(now))
	require.True(t, ev2.when.After(now))

	// With a small backoffMax, the scheduled gap should not exceed ~backoffMax + jitter
	// (jitter is eff/10; eff capped to backoffMax).
	// We can't read the exact interval from the event, but we can bound the first one.
	// Allow some slop for timing; just ensure it's not wildly larger than cap*1.5.
	require.LessOrEqual(t, time.Until(ev1.when), time.Duration(float64(150*time.Millisecond)*1.5))
}
