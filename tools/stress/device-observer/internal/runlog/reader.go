// Package runlog reads the orchestrator's runlog (orchestrator-runlog.jsonl)
// via tailer.Tailer and maintains bounded rings of provision and deprovision
// durations for the abort decider (PR #3796).
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
	// ringCapacity bounds memory for a very long sweep. 1024 covers ~10x the
	// expected ~100-user sweep with headroom for retries.
	ringCapacity = 1024
	// maxPending caps each pending-submit map. A misbehaving orchestrator
	// that emits submits without matching activates would otherwise grow
	// these maps until OOM. On overflow we evict the oldest entry.
	maxPending = 4096
)

// Row mirrors the orchestrator's runlog schema. We re-declare it here so
// the observer does not take a build-time dep on tools/stress/device-orchestrator.
type Row struct {
	UserIndex int    `json:"user_index"`
	Event     string `json:"event"`
	TNs       int64  `json:"t_ns"`
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
		switch row.Event {
		case "submit":
			insertPending(r.pendingSubmit, row.UserIndex, at)
		case "activate":
			if start, ok := r.pendingSubmit[row.UserIndex]; ok {
				delete(r.pendingSubmit, row.UserIndex)
				r.provisionRing = pushRing(r.provisionRing, durationSample{at: at, dur: at.Sub(start)})
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

// insertPending caps the pending map at maxPending entries. On overflow it
// evicts the oldest entry by scanning the map (O(maxPending) per overflow,
// which is acceptable since the map is bounded). Without this cap a
// misbehaving orchestrator that never emits matching activates would grow
// the map until OOM.
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

// truncateForLog bounds a log field so a flood of large malformed lines
// cannot balloon the observer's own log.
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
	// Drop the oldest by copying the tail forward in place.
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
