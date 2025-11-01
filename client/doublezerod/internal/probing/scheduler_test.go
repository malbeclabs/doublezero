package probing

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProbing_IntervalScheduler_AddPeekPopLeaseAndComplete(t *testing.T) {
	t.Parallel()

	s, err := NewIntervalScheduler(100*time.Millisecond, 0.0, false)
	require.NoError(t, err)

	base := time.Unix(0, 0)

	k1 := RouteKey{table: 10, dst: netip.MustParseAddr("10.0.0.1"), nextHop: netip.MustParseAddr("192.0.2.1")}
	k2 := RouteKey{table: 10, dst: netip.MustParseAddr("10.0.0.2"), nextHop: netip.MustParseAddr("192.0.2.1")}

	s.Add(k1, base)
	s.Add(k2, base)
	require.Equal(t, 2, s.Len())

	// Before due time: nothing should pop.
	due, ok := s.Peek()
	require.True(t, ok)
	require.True(t, due.After(base))
	out := s.PopDue(base.Add(50 * time.Millisecond))
	require.Len(t, out, 0)

	// After due time: both should pop and be leased (in-flight).
	now := base.Add(150 * time.Millisecond)
	out = s.PopDue(now)
	require.Len(t, out, 2)
	require.ElementsMatch(t, []RouteKey{k1, k2}, []RouteKey{out[0], out[1]})

	// While in-flight (before Complete), Peek should have nothing due.
	_, ok = s.Peek()
	require.False(t, ok)

	// Complete k1 at now; it should be re-armed exactly +interval (no jitter).
	s.Complete(k1, ProbeOutcome{OK: true, When: now})
	next, ok := s.Peek()
	require.True(t, ok)
	require.WithinDuration(t, now.Add(100*time.Millisecond), next, 2*time.Millisecond)

	// Complete k2 a bit later; it should re-arm relative to its own completion time.
	later := now.Add(20 * time.Millisecond)
	s.Complete(k2, ProbeOutcome{OK: true, When: later})
	next2, ok := s.Peek()
	require.True(t, ok)
	// The earliest next due should be k1 @ now+100ms, which is before k2 @ later+100ms.
	require.WithinDuration(t, now.Add(100*time.Millisecond), next2, 2*time.Millisecond)
}

func TestProbing_IntervalScheduler_PhaseSpreadsFirstDue(t *testing.T) {
	t.Parallel()

	s, err := NewIntervalScheduler(1*time.Second, 0.0, true)
	require.NoError(t, err)

	base := time.Unix(100, 0)
	keys := []RouteKey{
		{table: 1, dst: netip.MustParseAddr("10.0.0.1"), nextHop: netip.MustParseAddr("203.0.113.1")},
		{table: 1, dst: netip.MustParseAddr("10.0.0.2"), nextHop: netip.MustParseAddr("203.0.113.1")},
		{table: 1, dst: netip.MustParseAddr("10.0.0.3"), nextHop: netip.MustParseAddr("203.0.113.1")},
		{table: 1, dst: netip.MustParseAddr("10.0.0.4"), nextHop: netip.MustParseAddr("203.0.113.1")},
	}
	for _, k := range keys {
		s.Add(k, base)
	}
	require.Equal(t, len(keys), s.Len())

	// Far-future pop returns all (their first dues are within the first interval window).
	all := s.PopDue(base.Add(10 * time.Second))
	require.Len(t, all, len(keys))
	require.ElementsMatch(t, []RouteKey{keys[0], keys[1], keys[2], keys[3]},
		[]RouteKey{all[0], all[1], all[2], all[3]})

	// Assert phasing actually distributes offsets deterministically: at least 2 distinct offsets.
	offsets := make(map[time.Duration]struct{})
	for _, k := range keys {
		offsets[phaseOffset(1*time.Second, k)] = struct{}{}
	}
	require.GreaterOrEqual(t, len(offsets), 2, "phase should distribute first-due offsets across the interval")
}

func TestProbing_IntervalScheduler_DelAndClear(t *testing.T) {
	t.Parallel()

	s, err := NewIntervalScheduler(200*time.Millisecond, 0.0, false)
	require.NoError(t, err)

	base := time.Unix(0, 0)
	k1 := RouteKey{table: 7, dst: netip.MustParseAddr("10.1.0.1"), nextHop: netip.MustParseAddr("198.51.100.1")}
	k2 := RouteKey{table: 7, dst: netip.MustParseAddr("10.1.0.2"), nextHop: netip.MustParseAddr("198.51.100.1")}

	s.Add(k1, base)
	s.Add(k2, base)
	require.Equal(t, 2, s.Len())

	ok := s.Del(k1)
	require.True(t, ok)
	require.Equal(t, 1, s.Len())

	// k1 should never appear due anymore.
	out := s.PopDue(base.Add(5 * time.Second))
	require.Len(t, out, 1)
	require.Equal(t, k2, out[0])

	// Clear should remove everything.
	s.Clear()
	require.Equal(t, 0, s.Len())
	_, ok = s.Peek()
	require.False(t, ok)
}

func TestProbing_IntervalScheduler_LeasePreventsNonstopLoop(t *testing.T) {
	t.Parallel()

	s, err := NewIntervalScheduler(50*time.Millisecond, 0.0, false)
	require.NoError(t, err)

	base := time.Unix(0, 0)
	k := RouteKey{table: 1, dst: netip.MustParseAddr("10.2.0.1"), nextHop: netip.MustParseAddr("192.0.2.1")}
	s.Add(k, base)

	now := base.Add(100 * time.Millisecond)
	due := s.PopDue(now)
	require.Len(t, due, 1)

	// Without Complete, another PopDue at a later "now" should NOT return it again (leased).
	later := now.Add(1 * time.Second)
	none := s.PopDue(later)
	require.Len(t, none, 0, "item should be leased until Complete is called")

	// After Complete, it should schedule again.
	s.Complete(k, ProbeOutcome{OK: true, When: later})
	next, ok := s.Peek()
	require.True(t, ok)
	require.WithinDuration(t, later.Add(50*time.Millisecond), next, 2*time.Millisecond)
}

func TestProbing_IntervalScheduler_JitterBounds(t *testing.T) {
	t.Parallel()

	const iv = 200 * time.Millisecond
	const jit = 0.5 // factor in [0.5, 1.5]

	s, err := NewIntervalScheduler(iv, jit, false)
	require.NoError(t, err)

	base := time.Unix(0, 0)
	k := RouteKey{table: 9, dst: netip.MustParseAddr("10.9.0.9"), nextHop: netip.MustParseAddr("203.0.113.9")}
	s.Add(k, base)

	_ = s.PopDue(base.Add(10 * time.Second)) // pull first cycle, leasing it

	when := base.Add(3 * time.Second) // fixed "now" for determinism
	s.Complete(k, ProbeOutcome{OK: true, When: when})

	next, ok := s.Peek()
	require.True(t, ok)

	// With jitter=0.5, next interval ∈ [0.5*iv, 1.5*iv]
	low := when.Add(time.Duration(0.5 * float64(iv)))
	high := when.Add(time.Duration(1.5 * float64(iv)))
	require.True(t, !next.Before(low) && !next.After(high),
		"next due %v should be within [%v, %v]", next, low, high)
}
