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
		state:          Up,
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
	require.Equal(t, Down, sess.state)
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
		state:          Up,
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
		state:      AdminDown,
		alive:      true,
		detectMult: 1,
		minTxFloor: time.Millisecond,
	}
	s.scheduleTx(time.Now(), sess)
	require.Nil(t, s.eq.Pop(), "no TX should be scheduled while AdminDown")
}
