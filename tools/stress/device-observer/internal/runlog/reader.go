// Package runlog reads the orchestrator's runlog (orchestrator-runlog.jsonl)
// via tailer.Tailer and maintains bounded rings of provision and deprovision
// durations for the abort decider.
package runlog

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"
	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/tailer"
)

const (
	inputFilename = "orchestrator-runlog.jsonl"
	ringCapacity  = 1024 // bounded duration ring; ~10x a typical sweep
	maxPending    = 4096 // cap per pending-submit map; evicts oldest on overflow
)

// Row mirrors the orchestrator's runlog schema (redeclared to avoid a
// build-time dep on tools/stress/device-orchestrator). NAfterEvent is the
// orchestrator's view of how many users it considers active after this
// event; tracked here so the abort decider can compare it against the
// device's actual tunnel count and catch silent provisioning gaps.
type Row struct {
	UserIndex   int    `json:"user_index"`
	Event       string `json:"event"`
	TNs         int64  `json:"t_ns"`
	NAfterEvent int    `json:"n_after_event"`
}

// Reader tails the orchestrator runlog and exposes pair-completion duration
// rings for the provision (submit → activate) and deprovision flows.
type Reader struct {
	inPath   string
	interval time.Duration
	logger   *slog.Logger
	tail     *tailer.Tailer

	mu                  sync.RWMutex
	pendingSubmit       map[int]time.Time
	pendingDeprovSubmit map[int]time.Time
	provisionRing       []durationSample
	deprovisionRing     []durationSample

	// activeCount tracks the orchestrator's most-recent n_after_event from
	// the runlog (any event qualifies — it's monotonically maintained on the
	// orchestrator side as it provisions and deprovisions). lastActivateAt
	// pins the wall-clock timestamp of the most recent provision activate
	// so the decider can apply a grace window before comparing against the
	// device's actual tunnel count.
	activeCount     int
	activeCountSeen bool
	lastActivateAt  time.Time
}

type durationSample struct {
	at  time.Time
	dur time.Duration
}

func New(workingDir string, interval time.Duration, logger *slog.Logger) *Reader {
	inPath := filepath.Join(workingDir, inputFilename)
	return &Reader{
		inPath:              inPath,
		interval:            interval,
		logger:              logger,
		tail:                tailer.New(inPath),
		pendingSubmit:       map[int]time.Time{},
		pendingDeprovSubmit: map[int]time.Time{},
	}
}

func (r *Reader) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	defer r.tail.Close()
	r.tick()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.tick()
		}
	}
}

func (r *Reader) tick() {
	lines, err := r.tail.Poll()
	if err != nil {
		// ErrOversizeLine is non-fatal: any complete lines surfaced before
		// the overflow are still valid runlog rows.
		r.logger.Warn("runlog tail failed", "path", r.inPath, "err", err)
		if !errors.Is(err, tailer.ErrOversizeLine) {
			return
		}
	}
	if len(lines) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range lines {
		var row Row
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			// Truncate the logged line so a flood of malformed rows cannot
			// inflate the observer's own log without bound.
			r.logger.Warn("runlog decode failed", "line", truncateForLog(line), "err", err)
			continue
		}
		at := time.Unix(0, row.TNs)
		// Track the latest active-user count across all events. The
		// orchestrator emits n_after_event on every row and progresses it
		// linearly through provision and deprovision, so the most recent
		// row is always authoritative.
		r.activeCount = row.NAfterEvent
		r.activeCountSeen = true
		switch row.Event {
		case "submit":
			insertPending(r.pendingSubmit, row.UserIndex, at)
		case "activate":
			if start, ok := r.pendingSubmit[row.UserIndex]; ok {
				delete(r.pendingSubmit, row.UserIndex)
				r.provisionRing = pushRing(r.provisionRing, durationSample{at: at, dur: at.Sub(start)})
			}
			if at.After(r.lastActivateAt) {
				r.lastActivateAt = at
			}
		case "deprovision_submit":
			insertPending(r.pendingDeprovSubmit, row.UserIndex, at)
		case "deprovision_activate":
			if start, ok := r.pendingDeprovSubmit[row.UserIndex]; ok {
				delete(r.pendingDeprovSubmit, row.UserIndex)
				r.deprovisionRing = pushRing(r.deprovisionRing, durationSample{at: at, dur: at.Sub(start)})
			}
		}
	}
}

// insertPending caps the pending map at maxPending entries, evicting the
// oldest on overflow.
func insertPending(m map[int]time.Time, userIndex int, at time.Time) {
	if _, exists := m[userIndex]; !exists && len(m) >= maxPending {
		var oldestKey int
		var oldestAt time.Time
		first := true
		for k, v := range m {
			if first || v.Before(oldestAt) {
				oldestKey, oldestAt = k, v
				first = false
			}
		}
		delete(m, oldestKey)
	}
	m[userIndex] = at
}

func truncateForLog(s string) string {
	const max = 256
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func pushRing(ring []durationSample, s durationSample) []durationSample {
	if len(ring) < ringCapacity {
		return append(ring, s)
	}
	copy(ring, ring[1:])
	ring[len(ring)-1] = s
	return ring
}

// ProvisionDurations returns the durations of submit→activate pairs whose
// activate timestamp lies within window of the current wall clock. Returns
// an empty slice if no completions are in the window.
func (r *Reader) ProvisionDurations(window time.Duration) []time.Duration {
	return r.filterDurations(r.provisionRingSnapshot(), window)
}

// DeprovisionDurations is ProvisionDurations for the deprovision pair.
func (r *Reader) DeprovisionDurations(window time.Duration) []time.Duration {
	return r.filterDurations(r.deprovisionRingSnapshot(), window)
}

// ActiveUserCount returns the orchestrator's most recent n_after_event
// value alongside the wall-clock timestamp of the most recent activate
// event (i.e. the moment the count last grew via a provision). `ok` is
// false before any runlog row has been seen, so the abort decider can
// suppress the device_tunnel_gap trigger while the runlog file is still
// empty.
func (r *Reader) ActiveUserCount() (count int, lastActivate time.Time, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeCount, r.lastActivateAt, r.activeCountSeen
}

func (r *Reader) provisionRingSnapshot() []durationSample {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]durationSample, len(r.provisionRing))
	copy(out, r.provisionRing)
	return out
}

func (r *Reader) deprovisionRingSnapshot() []durationSample {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]durationSample, len(r.deprovisionRing))
	copy(out, r.deprovisionRing)
	return out
}

func (r *Reader) filterDurations(ring []durationSample, window time.Duration) []time.Duration {
	cutoff := time.Now().Add(-window)
	out := make([]time.Duration, 0, len(ring))
	for _, s := range ring {
		if s.at.Before(cutoff) {
			continue
		}
		out = append(out, s.dur)
	}
	return out
}

var _ collector.Collector = (*Reader)(nil)
