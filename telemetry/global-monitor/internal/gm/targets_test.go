package gm

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
)

func TestGlobalMonitor_TargetSet_ConfigValidate(t *testing.T) {
	t.Parallel()

	cfg := &TargetSetConfig{}
	require.Error(t, cfg.Validate())

	cfg.Clock = clockwork.NewFakeClock()
	require.Error(t, cfg.Validate())

	cfg.ProbeTimeout = time.Second
	require.Error(t, cfg.Validate())

	cfg.MaxConcurrency = 1
	require.NoError(t, cfg.Validate())
}

func TestGlobalMonitor_TargetSet_LenAndUpdateNil(t *testing.T) {
	t.Parallel()

	ts := newTargetSetForTest(t, clockwork.NewFakeClock(), time.Second, 4)
	require.Equal(t, 0, ts.Len())

	ts.Update(nil)
	require.Equal(t, 0, ts.Len())
}

func TestGlobalMonitor_TargetSet_PruneClosesRemovedTargets(t *testing.T) {
	t.Parallel()

	ts := newTargetSetForTest(t, clockwork.NewFakeClock(), time.Second, 4)

	a := &testProbeTarget{id: "a"}
	b := &testProbeTarget{id: "b"}
	c := &testProbeTarget{id: "c"}

	ts.Update(map[ProbeTargetID]ProbeTarget{
		a.ID(): a,
		b.ID(): b,
		c.ID(): c,
	})
	require.Equal(t, 3, ts.Len())

	ts.Prune(map[ProbeTargetID]ProbeTarget{
		a.ID(): a,
		c.ID(): c,
	})
	require.Equal(t, 2, ts.Len())
	require.Equal(t, int32(0), a.ClosedCount())
	require.Equal(t, int32(1), b.ClosedCount())
	require.Equal(t, int32(0), c.ClosedCount())
}

func TestGlobalMonitor_TargetSet_UpdateReusesExistingTargetInstances(t *testing.T) {
	t.Parallel()

	ts := newTargetSetForTest(t, clockwork.NewFakeClock(), time.Second, 4)

	orig := &testProbeTarget{id: "x"}
	ts.Update(map[ProbeTargetID]ProbeTarget{orig.ID(): orig})
	require.Equal(t, 1, ts.Len())

	replacement := &testProbeTarget{id: "x"}
	ts.Update(map[ProbeTargetID]ProbeTarget{replacement.ID(): replacement})

	ts.mu.Lock()
	got := ts.byID[ProbeTargetID("x")]
	ts.mu.Unlock()

	require.Same(t, orig, got)
	require.Equal(t, int32(0), orig.ClosedCount())
	require.Equal(t, int32(0), replacement.ClosedCount())
}

func TestGlobalMonitor_TargetSet_ExecuteProbes_RecordsResults(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClockAt(time.Unix(1000, 0))
	ts := newTargetSetForTest(t, clk, time.Second, 4)

	a := &testProbeTarget{id: "a", probeFn: func(ctx context.Context) (*ProbeResult, error) {
		return &ProbeResult{OK: true}, nil
	}}
	b := &testProbeTarget{id: "b", probeFn: func(ctx context.Context) (*ProbeResult, error) {
		return &ProbeResult{OK: false, FailReason: ProbeFailReasonOther}, nil
	}}

	ts.Update(map[ProbeTargetID]ProbeTarget{a.ID(): a, b.ID(): b})

	results, err := ts.ExecuteProbes(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Contains(t, results, a.ID())
	require.Contains(t, results, b.ID())
	require.Equal(t, time.Unix(1000, 0), results[a.ID()].Timestamp)
	require.Equal(t, time.Unix(1000, 0), results[b.ID()].Timestamp)
	require.True(t, results[a.ID()].OK)
	require.False(t, results[b.ID()].OK)
}

func TestGlobalMonitor_TargetSet_ExecuteProbes_DeadlineExceededBecomesTimeoutFailure(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClockAt(time.Unix(1000, 0))
	ts := newTargetSetForTest(t, clk, 10*time.Millisecond, 4)

	a := &testProbeTarget{id: "a", probeFn: func(ctx context.Context) (*ProbeResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	ts.Update(map[ProbeTargetID]ProbeTarget{a.ID(): a})

	results, err := ts.ExecuteProbes(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	res := results[a.ID()]
	require.NotNil(t, res)
	require.Equal(t, ProbeFailReasonTimeout, res.FailReason)
	require.NotNil(t, res.FailError)
	require.Equal(t, time.Unix(1000, 0), res.Timestamp)
}

func TestGlobalMonitor_TargetSet_ExecuteProbes_ContextCanceledSkipsResult(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClockAt(time.Unix(1000, 0))
	ts := newTargetSetForTest(t, clk, time.Second, 4)

	a := &testProbeTarget{id: "a", probeFn: func(ctx context.Context) (*ProbeResult, error) {
		return nil, context.Canceled
	}}
	ts.Update(map[ProbeTargetID]ProbeTarget{a.ID(): a})

	results, err := ts.ExecuteProbes(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 0)
}

func TestGlobalMonitor_TargetSet_ExecuteProbes_NonContextErrorSkipsResult(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClockAt(time.Unix(1000, 0))
	ts := newTargetSetForTest(t, clk, time.Second, 4)

	a := &testProbeTarget{id: "a", probeFn: func(ctx context.Context) (*ProbeResult, error) {
		return nil, io.ErrUnexpectedEOF
	}}
	ts.Update(map[ProbeTargetID]ProbeTarget{a.ID(): a})

	results, err := ts.ExecuteProbes(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 0)
}

func TestGlobalMonitor_TargetSet_ExecuteProbes_NilResultIsSkipped(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClockAt(time.Unix(1000, 0))
	ts := newTargetSetForTest(t, clk, time.Second, 4)

	a := &testProbeTarget{id: "a", probeFn: func(ctx context.Context) (*ProbeResult, error) {
		return nil, nil
	}}
	ts.Update(map[ProbeTargetID]ProbeTarget{a.ID(): a})

	results, err := ts.ExecuteProbes(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 0)
}

func TestGlobalMonitor_TargetSet_ExecuteProbes_RespectsMaxConcurrency(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClockAt(time.Unix(1000, 0))
	ts := newTargetSetForTest(t, clk, time.Second, 2)

	var inFlight int32
	var maxSeen int32

	start := make(chan struct{})
	release := make(chan struct{})

	mk := func(id string) *testProbeTarget {
		return &testProbeTarget{
			id: ProbeTargetID(id),
			probeFn: func(ctx context.Context) (*ProbeResult, error) {
				<-start
				cur := atomic.AddInt32(&inFlight, 1)
				for {
					prev := atomic.LoadInt32(&maxSeen)
					if cur <= prev || atomic.CompareAndSwapInt32(&maxSeen, prev, cur) {
						break
					}
				}
				<-release
				atomic.AddInt32(&inFlight, -1)
				return &ProbeResult{OK: true}, nil
			},
		}
	}

	t1 := mk("t1")
	t2 := mk("t2")
	t3 := mk("t3")
	t4 := mk("t4")
	ts.Update(map[ProbeTargetID]ProbeTarget{
		t1.ID(): t1, t2.ID(): t2, t3.ID(): t3, t4.ID(): t4,
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = ts.ExecuteProbes(context.Background())
	}()

	close(start)
	time.Sleep(20 * time.Millisecond)
	require.LessOrEqual(t, atomic.LoadInt32(&maxSeen), int32(2))

	close(release)
	wg.Wait()
}

func TestGlobalMonitor_TargetSet_Prune_RespectsMaxConcurrency(t *testing.T) {
	t.Parallel()

	ts := newTargetSetForTest(t, clockwork.NewFakeClock(), time.Second, 2)

	var inFlight int32
	var maxSeen int32
	block := make(chan struct{})

	mk := func(id string) *testProbeTarget {
		return &testProbeTarget{
			id: ProbeTargetID(id),
			closeFn: func() {
				cur := atomic.AddInt32(&inFlight, 1)
				for {
					prev := atomic.LoadInt32(&maxSeen)
					if cur <= prev || atomic.CompareAndSwapInt32(&maxSeen, prev, cur) {
						break
					}
				}
				<-block
				atomic.AddInt32(&inFlight, -1)
			},
		}
	}

	a := mk("a")
	b := mk("b")
	c := mk("c")
	d := mk("d")
	ts.Update(map[ProbeTargetID]ProbeTarget{a.ID(): a, b.ID(): b, c.ID(): c, d.ID(): d})
	require.Equal(t, 4, ts.Len())

	done := make(chan struct{})
	go func() {
		ts.Prune(map[ProbeTargetID]ProbeTarget{})
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	require.LessOrEqual(t, atomic.LoadInt32(&maxSeen), int32(2))

	close(block)
	<-done
	require.Equal(t, 0, ts.Len())
}

type testProbeTarget struct {
	id ProbeTargetID

	probeFn func(ctx context.Context) (*ProbeResult, error)
	closeFn func()

	closeCount int32
}

func (t *testProbeTarget) ID() ProbeTargetID { return t.id }
func (t *testProbeTarget) Probe(ctx context.Context) (*ProbeResult, error) {
	if t.probeFn == nil {
		return &ProbeResult{OK: true}, nil
	}
	return t.probeFn(ctx)
}
func (t *testProbeTarget) Close() {
	atomic.AddInt32(&t.closeCount, 1)
	if t.closeFn != nil {
		t.closeFn()
	}
}
func (t *testProbeTarget) ClosedCount() int32 { return atomic.LoadInt32(&t.closeCount) }

func newTargetSetForTest(t *testing.T, clk clockwork.Clock, timeout time.Duration, maxConc int) *TargetSet {
	t.Helper()
	ts, err := NewTargetSet(newTestLogger(), &TargetSetConfig{
		Clock:            clk,
		ProbeTimeout:     timeout,
		MaxConcurrency:   maxConc,
		VerboseFailures:  true,
		VerboseSuccesses: true,
	})
	require.NoError(t, err)
	return ts
}
