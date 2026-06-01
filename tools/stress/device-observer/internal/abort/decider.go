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

	// Triggers gated on a per-tick sample count below which a single
	// outlier could trip the orchestrator on startup or an empty batch.
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

	metricApplyConfigErrors = "doublezero_agent_apply_config_errors_total"
	metricGetConfigErrors   = "doublezero_agent_get_config_errors_total"
)

// Sources reads from the other collectors. Function-typed so tests pass
// fakes without spinning up the real collectors.
type Sources struct {
	PromSnapshot         func() map[string]float64
	AgentSnapshot        func() loggingtail.AgentSnapshot
	ProvisionDurations   func(time.Duration) []time.Duration
	DeprovisionDurations func(time.Duration) []time.Duration
	CPUPercent           func() (float64, bool)
	LedgerHeartbeatPath  string
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
				d.fire(TriggerProvisionP95, fmt.Sprintf("provision p95 %s exceeded %s over %d samples", p95, p95ProvisionThresh, len(durs)))
				return
			}
		}
		for _, dur := range durs {
			if dur > singleUserThresh {
				d.fire(TriggerProvisionSingleUser, fmt.Sprintf("provision duration %s exceeded %s", dur, singleUserThresh))
				return
			}
		}
	}

	if f := d.cfg.Sources.DeprovisionDurations; f != nil {
		durs := f(batchWindow)
		if len(durs) >= minSamples {
			if p95 := percentile95(durs); p95 > p95DeprovisionThresh {
				d.fire(TriggerDeprovisionP95, fmt.Sprintf("deprovision p95 %s exceeded %s over %d samples", p95, p95DeprovisionThresh, len(durs)))
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
			d.fire(TriggerCPUSustained, fmt.Sprintf("CPU >= %.0f%% sustained over %s", cpuPercentThresh, cpuSustainedWindow))
			return
		}
	}

	if f := d.cfg.Sources.PromSnapshot; f != nil {
		counters := f()
		if d.countersSeeded {
			for _, c := range []struct{ name, trigger string }{
				{metricApplyConfigErrors, TriggerApplyConfigErrors},
				{metricGetConfigErrors, TriggerGetConfigErrors},
			} {
				prev, cur := d.prevCounters[c.name], counters[c.name]
				if cur > prev {
					d.prevCounters = counters
					d.fire(c.trigger, fmt.Sprintf("%s incremented %g→%g", c.name, prev, cur))
					return
				}
			}
		}
		d.prevCounters = counters
		d.countersSeeded = true
	}

	if f := d.cfg.Sources.AgentSnapshot; f != nil {
		snap := f()
		if d.patternsSeeded {
			for _, p := range []struct{ name, trigger string }{
				{loggingtail.PatternDiffTimeout, TriggerDiffTimeout},
				{loggingtail.PatternLockNotTaken, TriggerLockNotTaken},
			} {
				if cur, prev := snap.MatchCounts[p.name], d.prevPatterns[p.name]; cur > prev {
					d.prevPatterns = copyIntMap(snap.MatchCounts)
					d.fire(p.trigger, fmt.Sprintf("agent log pattern %q observed %d→%d", p.name, prev, cur))
					return
				}
			}
		}
		d.prevPatterns = copyIntMap(snap.MatchCounts)
		d.patternsSeeded = true

		// Skip while LastLineAt is zero so we don't false-fire before
		// the orchestrator has started the agent.
		if !snap.LastLineAt.IsZero() && now.Sub(snap.LastLineAt) > agentSilenceThresh {
			d.fire(TriggerAgentSilence, fmt.Sprintf("agent silent for %s (last line at %s)", now.Sub(snap.LastLineAt), snap.LastLineAt.UTC().Format(time.RFC3339Nano)))
			return
		}
	}

	if path := d.cfg.Sources.LedgerHeartbeatPath; path != "" {
		st, err := os.Stat(path)
		if err == nil {
			if age := now.Sub(st.ModTime()); age > ledgerStaleThresh {
				d.fire(TriggerLedgerHeartbeatStale, fmt.Sprintf("ledger heartbeat %s stale by %s", path, age))
				return
			}
		} else if !os.IsNotExist(err) {
			d.cfg.Logger.Warn("ledger heartbeat stat failed", "path", path, "err", err)
		}
	}
}

func (d *Decider) cpuSustained(now time.Time) bool {
	cutoff := now.Add(-cpuSustainedWindow)
	kept := d.cpuRing[:0]
	for _, s := range d.cpuRing {
		if s.at.Before(cutoff) {
			continue
		}
		kept = append(kept, s)
	}
	d.cpuRing = kept
	if len(d.cpuRing) < minSamples {
		return false
	}
	for _, s := range d.cpuRing {
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
// write does not strand the decider: the next tick will retry. Caller
// holds d.mu.
func (d *Decider) fire(trigger, detail string) {
	if d.fired {
		return
	}
	body, err := json.Marshal(sentinel{Reason: trigger, Detail: detail, FiredAtNs: d.cfg.now().UTC().UnixNano(), Trigger: trigger})
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

func copyIntMap(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

var _ collector.Collector = (*Decider)(nil)
