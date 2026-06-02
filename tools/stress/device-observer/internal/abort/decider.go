// Package abort evaluates abort signals from the device-observer's collectors
// and writes a single sentinel file when any trigger fires.
package abort

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"
	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/loggingtail"
)

const (
	p95ProvisionThresh   = 30 * time.Second
	p95DeprovisionThresh = 30 * time.Second
	singleUserThresh     = 30 * time.Second
	cpuPercentThresh     = 80.0
	cpuSustainedWindow   = 60 * time.Second
	agentSilenceThresh   = 15 * time.Second
	ledgerStaleThresh    = 30 * time.Second
	batchWindow          = 5 * time.Minute

	// startupGrace suppresses the counter- and log-pattern-based triggers
	// for the first window after the decider starts. The doublezero-agent
	// reliably emits an apply_config_errors / "could not get diff" race on
	// its first commit attempt as EOS validates the new session — a real
	// transient, not a sweep failure. The provision/deprovision and CPU
	// triggers are unaffected (they have their own minSamples / window
	// requirements). After grace, a single counter or pattern increment
	// fires the trigger as before.
	startupGrace = 60 * time.Second

	// deviceTunnelGapGrace is how long after a provision activate the
	// device is given to converge before the device_tunnel_gap trigger
	// inspects the runlog-vs-eAPI delta. A single batch's apply settles
	// in well under 10 s in our local devnet; 30 s is conservative.
	deviceTunnelGapGrace = 30 * time.Second
	// deviceTunnelGapThreshold is the minimum (active - tunnels) shortfall
	// that fires the trigger. Small transient mismatches mid-commit
	// shouldn't fire — anything past that is a real divergence (controller
	// cap, agent stuck, etc.).
	deviceTunnelGapThreshold = 4

	// minSamples gates per-tick triggers below which a single outlier
	// could trip the orchestrator on startup or an empty batch.
	minSamples = 4

	cpuRingCap = 256

	TriggerProvisionP95         = "provision_p95"
	TriggerProvisionSingleUser  = "provision_single_user"
	TriggerDeprovisionP95       = "deprovision_p95"
	TriggerCPUSustained         = "cpu_sustained"
	TriggerApplyConfigErrors    = "apply_config_errors"
	TriggerGetConfigErrors      = "get_config_errors"
	TriggerDiffTimeout          = "diff_timeout"
	TriggerLockNotTaken         = "lock_not_taken"
	TriggerAgentSilence         = "agent_silence"
	TriggerLedgerHeartbeatStale = "ledger_heartbeat_stale"
	TriggerDeviceTunnelGap      = "device_tunnel_gap"

	metricApplyConfigErrors = "doublezero_agent_apply_config_errors_total"
	metricGetConfigErrors   = "doublezero_agent_get_config_errors_total"
)

// Sources is function-typed so tests can pass fakes without spinning up
// the real collectors. Snapshot getters must return maps the decider can
// retain without further mutation by the caller.
type Sources struct {
	PromSnapshot         func() map[string]float64
	AgentSnapshot        func() loggingtail.AgentSnapshot
	ProvisionDurations   func(time.Duration) []time.Duration
	DeprovisionDurations func(time.Duration) []time.Duration
	CPUPercent           func() (float64, bool)
	// ActiveUserCount returns the orchestrator's most recent
	// n_after_event from the runlog (the count of users the orchestrator
	// considers active), the wall-clock timestamp of the most recent
	// provision activate event, and `ok=false` before any runlog row has
	// been seen.
	ActiveUserCount func() (count int, lastActivate time.Time, ok bool)
	// TunnelCount returns the most recently observed number of user
	// tunnels on the device (from `show gre tunnel static`). `ok=false`
	// before the first successful sample.
	TunnelCount         func() (int, bool)
	LedgerHeartbeatPath string
}

// Config configures a Decider. OnFire is called exactly once after the
// sentinel write succeeds; main wires it to the root context's cancel.
type Config struct {
	AbortFile string
	Interval  time.Duration
	Logger    *slog.Logger
	Sources   Sources
	OnFire    func()
	now       func() time.Time
}

type Decider struct {
	cfg Config

	mu             sync.Mutex
	startedAt      time.Time
	prevCounters   map[string]float64
	countersSeeded bool
	prevPatterns   map[string]int
	patternsSeeded bool
	cpuRing        []cpuSample
	fired          bool
}

type cpuSample struct {
	at  time.Time
	pct float64
}

func New(cfg Config) *Decider {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	return &Decider{
		cfg:          cfg,
		startedAt:    cfg.now(),
		prevCounters: map[string]float64{},
		prevPatterns: map[string]int{},
	}
}

func (d *Decider) Run(ctx context.Context) error {
	ticker := time.NewTicker(d.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			d.tick()
			d.mu.Lock()
			fired := d.fired
			d.mu.Unlock()
			if fired {
				return nil
			}
		}
	}
}

func (d *Decider) tick() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fired {
		return
	}
	now := d.cfg.now()

	// p95 is checked before single-user so the more-confident batch
	// trigger preempts when both apply; single-user covers the early
	// batch before minSamples completions land.
	if f := d.cfg.Sources.ProvisionDurations; f != nil {
		durs := f(batchWindow)
		if len(durs) >= minSamples {
			if p95 := percentile95(durs); p95 > p95ProvisionThresh {
				d.fire(now, TriggerProvisionP95, fmt.Sprintf("provision p95 %s exceeded %s over %d samples", p95, p95ProvisionThresh, len(durs)))
				return
			}
		}
		for _, dur := range durs {
			if dur > singleUserThresh {
				d.fire(now, TriggerProvisionSingleUser, fmt.Sprintf("provision duration %s exceeded %s", dur, singleUserThresh))
				return
			}
		}
	}

	if f := d.cfg.Sources.DeprovisionDurations; f != nil {
		durs := f(batchWindow)
		if len(durs) >= minSamples {
			if p95 := percentile95(durs); p95 > p95DeprovisionThresh {
				d.fire(now, TriggerDeprovisionP95, fmt.Sprintf("deprovision p95 %s exceeded %s over %d samples", p95, p95DeprovisionThresh, len(durs)))
				return
			}
		}
	}

	if f := d.cfg.Sources.CPUPercent; f != nil {
		if pct, ok := f(); ok {
			d.cpuRing = append(d.cpuRing, cpuSample{at: now, pct: pct})
			if len(d.cpuRing) > cpuRingCap {
				d.cpuRing = d.cpuRing[len(d.cpuRing)-cpuRingCap:]
			}
		}
		if d.cpuSustained(now) {
			d.fire(now, TriggerCPUSustained, fmt.Sprintf("CPU >= %.0f%% sustained over %s", cpuPercentThresh, cpuSustainedWindow))
			return
		}
	}

	// Counter and log-pattern triggers absorb the agent's startup race for
	// `startupGrace` after the decider starts: keep reseeding `prev*` so
	// the increments accumulated during that window become the new
	// baseline, and don't fire. After grace, the existing increment-fires
	// logic applies against the latest baseline.
	pastGrace := now.Sub(d.startedAt) >= startupGrace

	if f := d.cfg.Sources.PromSnapshot; f != nil {
		counters := f()
		if d.countersSeeded && pastGrace {
			for _, c := range []struct{ name, trigger string }{
				{metricApplyConfigErrors, TriggerApplyConfigErrors},
				{metricGetConfigErrors, TriggerGetConfigErrors},
			} {
				prev, cur := d.prevCounters[c.name], counters[c.name]
				if cur > prev {
					d.prevCounters = copyFloatMap(counters)
					d.fire(now, c.trigger, fmt.Sprintf("%s incremented %g→%g", c.name, prev, cur))
					return
				}
			}
		}
		d.prevCounters = copyFloatMap(counters)
		d.countersSeeded = true
	}

	if f := d.cfg.Sources.AgentSnapshot; f != nil {
		snap := f()
		if d.patternsSeeded && pastGrace {
			for _, p := range []struct{ name, trigger string }{
				{loggingtail.PatternDiffTimeout, TriggerDiffTimeout},
				{loggingtail.PatternLockNotTaken, TriggerLockNotTaken},
			} {
				if cur, prev := snap.MatchCounts[p.name], d.prevPatterns[p.name]; cur > prev {
					d.prevPatterns = copyIntMap(snap.MatchCounts)
					d.fire(now, p.trigger, fmt.Sprintf("agent log pattern %q observed %d→%d", p.name, prev, cur))
					return
				}
			}
		}
		d.prevPatterns = copyIntMap(snap.MatchCounts)
		d.patternsSeeded = true

		// Skip while LastLineAt is zero so we don't false-fire before
		// the orchestrator has started the agent.
		if !snap.LastLineAt.IsZero() && now.Sub(snap.LastLineAt) > agentSilenceThresh {
			d.fire(now, TriggerAgentSilence, fmt.Sprintf("agent silent for %s (last line at %s)", now.Sub(snap.LastLineAt), snap.LastLineAt.UTC().Format(time.RFC3339Nano)))
			return
		}
	}

	// device_tunnel_gap: catch the orchestrator's view of active users
	// drifting above what the device actually has plumbed (controller
	// truncating the rendered config beyond its slot cap, agent stuck
	// without erroring, etc.). Suppressed until the most recent activate
	// is at least deviceTunnelGapGrace old so the agent has time to
	// converge. Suppressed entirely while either source is unset.
	if d.cfg.Sources.ActiveUserCount != nil && d.cfg.Sources.TunnelCount != nil {
		active, lastActivate, runlogOK := d.cfg.Sources.ActiveUserCount()
		tunnels, tunnelsOK := d.cfg.Sources.TunnelCount()
		if runlogOK && tunnelsOK && active > 0 && !lastActivate.IsZero() &&
			now.Sub(lastActivate) >= deviceTunnelGapGrace &&
			active-tunnels >= deviceTunnelGapThreshold {
			d.fire(now, TriggerDeviceTunnelGap, fmt.Sprintf("orchestrator reports %d active users but device has %d tunnels (gap %d, last activate %s ago)",
				active, tunnels, active-tunnels, now.Sub(lastActivate).Truncate(time.Second)))
			return
		}
	}

	if path := d.cfg.Sources.LedgerHeartbeatPath; path != "" {
		st, err := os.Stat(path)
		if err == nil {
			if age := now.Sub(st.ModTime()); age > ledgerStaleThresh {
				d.fire(now, TriggerLedgerHeartbeatStale, fmt.Sprintf("ledger heartbeat %s stale by %s", path, age))
				return
			}
		} else if !os.IsNotExist(err) {
			d.cfg.Logger.Warn("ledger heartbeat stat failed", "path", path, "err", err)
		}
	}
}

// cpuSustained reports whether the CPU has been >= cpuPercentThresh for
// every retained sample, the retained samples span at least
// cpuSustainedWindow, and there are at least minSamples retained.
func (d *Decider) cpuSustained(now time.Time) bool {
	cutoff := now.Add(-cpuSustainedWindow)
	kept := make([]cpuSample, 0, len(d.cpuRing))
	for _, s := range d.cpuRing {
		if s.at.Before(cutoff) {
			continue
		}
		kept = append(kept, s)
	}
	d.cpuRing = kept
	if len(kept) < minSamples {
		return false
	}
	if now.Sub(kept[0].at) < cpuSustainedWindow {
		return false
	}
	for _, s := range kept {
		if s.pct < cpuPercentThresh {
			return false
		}
	}
	return true
}

type sentinel struct {
	Reason    string `json:"reason"`
	Detail    string `json:"detail"`
	FiredAtNs int64  `json:"fired_at_ns"`
	Trigger   string `json:"trigger"`
}

// fire writes the sentinel exactly once and invokes OnFire on success.
// d.fired is only set after both write and rename succeed, so a failed
// write does not strand the decider: the next tick will retry.
func (d *Decider) fire(now time.Time, trigger, detail string) {
	if d.fired {
		return
	}
	body, err := json.Marshal(sentinel{Reason: trigger, Detail: detail, FiredAtNs: now.UTC().UnixNano(), Trigger: trigger})
	if err != nil {
		d.cfg.Logger.Error("marshal abort sentinel", "err", err)
		return
	}
	tmp := d.cfg.AbortFile + ".tmp"
	if err := os.WriteFile(tmp, body, 0o640); err != nil {
		d.cfg.Logger.Error("write abort sentinel tmp", "path", tmp, "err", err)
		return
	}
	if err := os.Rename(tmp, d.cfg.AbortFile); err != nil {
		d.cfg.Logger.Error("rename abort sentinel", "from", tmp, "to", d.cfg.AbortFile, "err", err)
		return
	}
	d.fired = true
	d.cfg.Logger.Warn("abort sentinel written", "trigger", trigger, "detail", detail, "path", d.cfg.AbortFile)
	if d.cfg.OnFire != nil {
		d.cfg.OnFire()
	}
}

// percentile95 returns the 95th percentile by nearest-rank.
func percentile95(durs []time.Duration) time.Duration {
	cp := make([]time.Duration, len(durs))
	copy(cp, durs)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	rank := int(math.Ceil(0.95*float64(len(cp)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(cp) {
		rank = len(cp) - 1
	}
	return cp[rank]
}

func copyFloatMap(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyIntMap(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

var _ collector.Collector = (*Decider)(nil)
