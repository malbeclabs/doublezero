package abort

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/loggingtail"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestDecider returns a Decider with a fixed clock and a counter that
// the test can read to confirm OnFire ran. The clock starts at a stable
// reference; tests that need to step time wrap it in their own closure.
func newTestDecider(t *testing.T, sources Sources, fireCount *atomic.Int32, now func() time.Time) *Decider {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		AbortFile: filepath.Join(dir, "abort"),
		Interval:  time.Hour, // tests drive tick() directly
		Logger:    discardLogger(),
		Sources:   sources,
		OnFire: func() {
			if fireCount != nil {
				fireCount.Add(1)
			}
		},
		now: now,
	}
	return New(cfg)
}

func readSentinel(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode sentinel: %v", err)
	}
	return out
}

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestProvisionP95(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	// At least minSamples durations all > 30 s so p95 (checked before
	// single-user) fires the p95 trigger rather than single-user.
	durs := []time.Duration{31 * time.Second, 31 * time.Second, 31 * time.Second, 31 * time.Second}
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		ProvisionDurations: func(time.Duration) []time.Duration { return durs },
	}, &fire, fixedNow(now))
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerProvisionP95 {
		t.Fatalf("trigger = %v, want %s", got["trigger"], TriggerProvisionP95)
	}
	if fire.Load() != 1 {
		t.Fatalf("OnFire count = %d, want 1", fire.Load())
	}
}

// TestProvisionSingleUserFiresBeforeP95Samples confirms a single very
// slow sample fires before enough samples have accumulated for p95.
func TestProvisionSingleUserFiresBeforeP95Samples(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		// Only 3 samples — below minSamples for p95.
		ProvisionDurations: func(time.Duration) []time.Duration {
			return []time.Duration{5 * time.Second, 6 * time.Second, 31 * time.Second}
		},
	}, &fire, fixedNow(now))
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerProvisionSingleUser {
		t.Fatalf("trigger = %v", got["trigger"])
	}
}

func TestProvisionSingleUser(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		ProvisionDurations: func(time.Duration) []time.Duration {
			return []time.Duration{31 * time.Second}
		},
	}, &fire, fixedNow(now))
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerProvisionSingleUser {
		t.Fatalf("trigger = %v", got["trigger"])
	}
}

func TestDeprovisionP95(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		DeprovisionDurations: func(time.Duration) []time.Duration {
			return []time.Duration{
				5 * time.Second, 5 * time.Second, 5 * time.Second, 31 * time.Second,
			}
		},
	}, &fire, fixedNow(now))
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerDeprovisionP95 {
		t.Fatalf("trigger = %v", got["trigger"])
	}
}

func TestCPUSustainedAt80(t *testing.T) {
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var step atomic.Int64
	clock := func() time.Time {
		// Each tick advances by 10 s.
		return base.Add(time.Duration(step.Add(1)) * 10 * time.Second)
	}
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		CPUPercent: func() (float64, bool) { return 85, true },
	}, &fire, clock)
	// 7 ticks @ 10s = 70 s of sustained 85% CPU.
	for i := 0; i < 7; i++ {
		d.tick()
		if fire.Load() > 0 {
			break
		}
	}
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerCPUSustained {
		t.Fatalf("trigger = %v", got["trigger"])
	}
}

// TestCPUSustainedBelowSpan confirms the decider does not fire when ≥
// minSamples are above the threshold but the samples don't span the
// full cpuSustainedWindow yet.
func TestCPUSustainedBelowSpan(t *testing.T) {
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var step atomic.Int64
	d := newTestDecider(t, Sources{
		CPUPercent: func() (float64, bool) { return 99, true },
	}, nil, func() time.Time {
		return base.Add(time.Duration(step.Add(1)) * 5 * time.Second)
	})
	for i := 0; i < 4; i++ {
		d.tick()
	}
	if _, err := os.Stat(d.cfg.AbortFile); err == nil {
		t.Fatal("decider must not fire while samples span < cpuSustainedWindow")
	}
}

func TestApplyConfigErrorsCounter(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	current := map[string]float64{metricApplyConfigErrors: 0}
	d := newTestDecider(t, Sources{
		PromSnapshot: func() map[string]float64 {
			out := make(map[string]float64, len(current))
			for k, v := range current {
				out[k] = v
			}
			return out
		},
	}, &fire, fixedNow(now))
	d.startedAt = now.Add(-2 * startupGrace) // bypass startup grace
	d.tick()                                 // seeds prev
	if fire.Load() != 0 {
		t.Fatal("should not fire on first observation")
	}
	current[metricApplyConfigErrors] = 1
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerApplyConfigErrors {
		t.Fatalf("trigger = %v", got["trigger"])
	}
}

func TestGetConfigErrorsCounter(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	current := map[string]float64{metricGetConfigErrors: 0}
	d := newTestDecider(t, Sources{
		PromSnapshot: func() map[string]float64 {
			out := make(map[string]float64, len(current))
			for k, v := range current {
				out[k] = v
			}
			return out
		},
	}, &fire, fixedNow(now))
	d.startedAt = now.Add(-2 * startupGrace) // bypass startup grace
	d.tick()
	current[metricGetConfigErrors] = 5
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerGetConfigErrors {
		t.Fatalf("trigger = %v", got["trigger"])
	}
}

func TestDiffTimeoutPattern(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	counts := map[string]int{loggingtail.PatternDiffTimeout: 0}
	d := newTestDecider(t, Sources{
		AgentSnapshot: func() loggingtail.AgentSnapshot {
			cp := map[string]int{}
			for k, v := range counts {
				cp[k] = v
			}
			return loggingtail.AgentSnapshot{MatchCounts: cp, LastLineAt: now}
		},
	}, &fire, fixedNow(now))
	d.startedAt = now.Add(-2 * startupGrace) // bypass startup grace
	d.tick()                                 // seed
	counts[loggingtail.PatternDiffTimeout] = 1
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerDiffTimeout {
		t.Fatalf("trigger = %v", got["trigger"])
	}
}

func TestLockNotTakenPattern(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	counts := map[string]int{loggingtail.PatternLockNotTaken: 0}
	d := newTestDecider(t, Sources{
		AgentSnapshot: func() loggingtail.AgentSnapshot {
			cp := map[string]int{}
			for k, v := range counts {
				cp[k] = v
			}
			return loggingtail.AgentSnapshot{MatchCounts: cp, LastLineAt: now}
		},
	}, &fire, fixedNow(now))
	d.startedAt = now.Add(-2 * startupGrace) // bypass startup grace
	d.tick()
	counts[loggingtail.PatternLockNotTaken] = 2
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerLockNotTaken {
		t.Fatalf("trigger = %v", got["trigger"])
	}
}

// TestStartupGraceSuppressesCounterTrigger covers the grace contract:
// within `startupGrace` of the decider start, even a counter increment
// (and a corresponding agent log pattern increment) must not fire. After
// grace, the next increment fires against the post-grace baseline.
func TestStartupGraceSuppressesCounterTrigger(t *testing.T) {
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var step atomic.Int64
	// 20s per call: New() captures startedAt at t=base+20s; ticks 1-3
	// fall at +40s, +60s, +80s (deltas 20s, 40s, 60s from startedAt — all
	// still within the 60s grace, since the gate is `>= grace`); tick 4
	// at +100s (delta 80s) is past grace.
	clock := func() time.Time {
		return base.Add(time.Duration(step.Add(1)) * 20 * time.Second)
	}
	var fire atomic.Int32
	current := map[string]float64{metricApplyConfigErrors: 0}
	d := newTestDecider(t, Sources{
		PromSnapshot: func() map[string]float64 {
			out := make(map[string]float64, len(current))
			for k, v := range current {
				out[k] = v
			}
			return out
		},
	}, &fire, clock)
	// Tick 1: t=base+60s, seeds prev=0.
	d.tick()
	// Counter increments to 1 while still in grace (t=base+90s ≤ start+60s).
	current[metricApplyConfigErrors] = 1
	d.tick()
	if fire.Load() != 0 {
		t.Fatal("must not fire while within startup grace")
	}
	// Past grace (t=base+120s, start=base+30s → diff=90s > 60s) but counter unchanged.
	d.tick()
	if fire.Load() != 0 {
		t.Fatal("must not fire when counter unchanged since prior tick")
	}
	// Now counter advances post-grace → fires.
	current[metricApplyConfigErrors] = 2
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerApplyConfigErrors {
		t.Fatalf("trigger = %v", got["trigger"])
	}
}

// TestStartupGraceSuppressesPatternTrigger is the agent-log-pattern
// analogue of the counter grace test.
func TestStartupGraceSuppressesPatternTrigger(t *testing.T) {
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var step atomic.Int64
	clock := func() time.Time {
		return base.Add(time.Duration(step.Add(1)) * 20 * time.Second)
	}
	var fire atomic.Int32
	counts := map[string]int{loggingtail.PatternDiffTimeout: 0}
	d := newTestDecider(t, Sources{
		AgentSnapshot: func() loggingtail.AgentSnapshot {
			cp := map[string]int{}
			for k, v := range counts {
				cp[k] = v
			}
			// LastLineAt must lead the clock so agent_silence doesn't preempt.
			return loggingtail.AgentSnapshot{MatchCounts: cp, LastLineAt: base.Add(10 * time.Hour)}
		},
	}, &fire, clock)
	// Tick 1 (delta 20s, in grace): seed prev=0.
	d.tick()
	// Increment to 1 within grace (tick 2 at delta 40s).
	counts[loggingtail.PatternDiffTimeout] = 1
	d.tick()
	if fire.Load() != 0 {
		got := readSentinel(t, d.cfg.AbortFile)
		t.Fatalf("must not fire within startup grace (fired with %v)", got)
	}
	// Tick 3 at delta 60s — AT/past grace boundary but pattern unchanged.
	d.tick()
	if fire.Load() != 0 {
		t.Fatal("must not fire when pattern unchanged")
	}
	// Tick 4 at delta 80s — past grace, increment → fires.
	counts[loggingtail.PatternDiffTimeout] = 2
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerDiffTimeout {
		t.Fatalf("trigger = %v", got["trigger"])
	}
}

func TestAgentSilenceFifteenSeconds(t *testing.T) {
	lastLine := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	silent := lastLine.Add(20 * time.Second)
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		AgentSnapshot: func() loggingtail.AgentSnapshot {
			return loggingtail.AgentSnapshot{MatchCounts: map[string]int{}, LastLineAt: lastLine}
		},
	}, &fire, fixedNow(silent))
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerAgentSilence {
		t.Fatalf("trigger = %v", got["trigger"])
	}
}

func TestAgentSilenceSuppressedBeforeFirstLine(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		AgentSnapshot: func() loggingtail.AgentSnapshot {
			return loggingtail.AgentSnapshot{MatchCounts: map[string]int{}}
		},
	}, &fire, fixedNow(now))
	d.tick()
	if _, err := os.Stat(d.cfg.AbortFile); err == nil {
		t.Fatal("decider must not fire while LastLineAt is zero")
	}
}

func TestLedgerHeartbeatStale(t *testing.T) {
	dir := t.TempDir()
	hb := filepath.Join(dir, "orchestrator.ledger_heartbeat")
	if err := os.WriteFile(hb, []byte("x"), 0o640); err != nil {
		t.Fatalf("write hb: %v", err)
	}
	old := time.Now().Add(-90 * time.Second)
	if err := os.Chtimes(hb, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	var fire atomic.Int32
	d := newTestDecider(t, Sources{LedgerHeartbeatPath: hb}, &fire, time.Now)
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerLedgerHeartbeatStale {
		t.Fatalf("trigger = %v", got["trigger"])
	}
}

func TestLedgerHeartbeatAbsentSuppressed(t *testing.T) {
	var fire atomic.Int32
	d := newTestDecider(t, Sources{LedgerHeartbeatPath: "/nonexistent/path/heartbeat"}, &fire, time.Now)
	d.tick()
	if _, err := os.Stat(d.cfg.AbortFile); err == nil {
		t.Fatal("missing heartbeat file must not fire the decider")
	}
}

func TestSentinelWrittenOnce(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		ProvisionDurations: func(time.Duration) []time.Duration {
			return []time.Duration{31 * time.Second}
		},
	}, &fire, fixedNow(now))
	d.tick()
	d.tick()
	d.tick()
	if fire.Load() != 1 {
		t.Fatalf("OnFire count = %d, want 1", fire.Load())
	}
}

func TestSentinelAtomicRename(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		ProvisionDurations: func(time.Duration) []time.Duration {
			return []time.Duration{31 * time.Second}
		},
	}, &fire, fixedNow(now))
	d.tick()
	if _, err := os.Stat(d.cfg.AbortFile + ".tmp"); err == nil {
		t.Fatal(".tmp must not survive after the rename")
	}
	got := readSentinel(t, d.cfg.AbortFile)
	if _, ok := got["reason"]; !ok {
		t.Fatalf("sentinel missing reason: %v", got)
	}
	if _, ok := got["fired_at_ns"]; !ok {
		t.Fatalf("sentinel missing fired_at_ns: %v", got)
	}
}

func TestRunCancels(t *testing.T) {
	d := New(Config{
		AbortFile: filepath.Join(t.TempDir(), "abort"),
		Interval:  50 * time.Millisecond,
		Logger:    discardLogger(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	time.Sleep(80 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 s of cancel")
	}
}

func TestOnFireInvokedOnce(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		ProvisionDurations: func(time.Duration) []time.Duration {
			return []time.Duration{31 * time.Second}
		},
		DeprovisionDurations: func(time.Duration) []time.Duration {
			return []time.Duration{
				31 * time.Second, 31 * time.Second, 31 * time.Second, 31 * time.Second,
			}
		},
	}, &fire, fixedNow(now))
	d.tick()
	d.tick()
	if fire.Load() != 1 {
		t.Fatalf("OnFire count = %d, want 1", fire.Load())
	}
}

// TestSentinelWriteFailureRetries confirms a failed sentinel write does
// not strand the decider: the next tick re-evaluates and writes the
// sentinel once the path becomes writable.
func TestSentinelWriteFailureRetries(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	dir := t.TempDir()
	// AbortFile points inside a directory that does not yet exist, so
	// os.WriteFile of .tmp fails on the first tick.
	bad := filepath.Join(dir, "absent", "abort")
	d := New(Config{
		AbortFile: bad,
		Interval:  time.Hour,
		Logger:    discardLogger(),
		Sources: Sources{
			ProvisionDurations: func(time.Duration) []time.Duration {
				return []time.Duration{31 * time.Second}
			},
		},
		OnFire: func() { fire.Add(1) },
		now:    fixedNow(now),
	})
	d.tick()
	if fire.Load() != 0 {
		t.Fatalf("OnFire must not be called on failed write, got %d", fire.Load())
	}
	if _, err := os.Stat(bad); err == nil {
		t.Fatal("sentinel must not exist after failed write")
	}
	// Make the path writable.
	if err := os.MkdirAll(filepath.Dir(bad), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	d.tick()
	if fire.Load() != 1 {
		t.Fatalf("OnFire count after recovery = %d, want 1", fire.Load())
	}
	if _, err := os.Stat(bad); err != nil {
		t.Fatalf("sentinel must exist after recovery: %v", err)
	}
}

func TestPercentile95(t *testing.T) {
	cases := []struct {
		in   []time.Duration
		want time.Duration
	}{
		{[]time.Duration{1, 2, 3, 4}, 4},
		{[]time.Duration{1, 1, 1, 1, 1, 1, 1, 1, 1, 99}, 99},
		{[]time.Duration{5}, 5},
	}
	for _, c := range cases {
		got := percentile95(c.in)
		if got != c.want {
			t.Errorf("percentile95(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestDeviceTunnelGapFiresAfterGrace covers the case the trigger was
// designed for: the orchestrator's view of active users (from the runlog)
// exceeds the device's actual tunnel count, the grace window has elapsed
// since the most recent activate, and the shortfall is at or above the
// threshold.
func TestDeviceTunnelGapFiresAfterGrace(t *testing.T) {
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	// now is just past the grace window after lastActivate.
	now := base.Add(deviceTunnelGapGrace + time.Second)
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		ActiveUserCount: func() (int, time.Time, bool) { return 32, base, true },
		TunnelCount:     func() (int, bool) { return 16, true },
	}, &fire, fixedNow(now))
	d.tick()
	got := readSentinel(t, d.cfg.AbortFile)
	if got["trigger"] != TriggerDeviceTunnelGap {
		t.Fatalf("trigger = %v, want %s", got["trigger"], TriggerDeviceTunnelGap)
	}
}

// TestDeviceTunnelGapSuppressedWithinGrace confirms the trigger does not
// fire until deviceTunnelGapGrace has passed since the most recent
// activate, so a mid-batch transient mismatch can't trip the sweep.
func TestDeviceTunnelGapSuppressedWithinGrace(t *testing.T) {
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	now := base.Add(deviceTunnelGapGrace - time.Second)
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		ActiveUserCount: func() (int, time.Time, bool) { return 32, base, true },
		TunnelCount:     func() (int, bool) { return 16, true },
	}, &fire, fixedNow(now))
	d.tick()
	if fire.Load() != 0 {
		t.Fatal("must not fire within deviceTunnelGapGrace")
	}
}

// TestDeviceTunnelGapBelowThreshold confirms small mid-commit mismatches
// (e.g. one tunnel briefly absent during a re-stage) do not fire.
func TestDeviceTunnelGapBelowThreshold(t *testing.T) {
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	now := base.Add(2 * deviceTunnelGapGrace)
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		// shortfall = active - tunnels = deviceTunnelGapThreshold - 1
		ActiveUserCount: func() (int, time.Time, bool) {
			return deviceTunnelGapThreshold, base, true
		},
		TunnelCount: func() (int, bool) { return 1, true },
	}, &fire, fixedNow(now))
	d.tick()
	if fire.Load() != 0 {
		t.Fatal("must not fire when shortfall is below threshold")
	}
}

// TestDeviceTunnelGapSuppressedBeforeFirstActivate handles the period
// after the observer starts but before the orchestrator has activated
// any user — the runlog is empty so ActiveUserCount returns ok=false.
func TestDeviceTunnelGapSuppressedBeforeFirstActivate(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var fire atomic.Int32
	d := newTestDecider(t, Sources{
		ActiveUserCount: func() (int, time.Time, bool) {
			return 0, time.Time{}, false
		},
		TunnelCount: func() (int, bool) { return 0, true },
	}, &fire, fixedNow(now))
	d.tick()
	if fire.Load() != 0 {
		t.Fatal("must not fire before any activate has been seen")
	}
}
