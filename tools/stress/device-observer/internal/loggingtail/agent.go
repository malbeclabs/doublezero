package loggingtail

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"
	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/tailer"
)

const (
	agentInputFilename  = "orchestrator.agent.log"
	agentOutputFilename = "observer.agent_log.json"
)

// Pattern names exposed via Snapshot. Naming is stable so the abort
// decider can reference them by string.
const (
	PatternDiffTimeout   = "diff_timeout"
	PatternLockNotTaken  = "lock_not_taken"
	PatternCommitSession = "commit_session"
)

var agentPatterns = []struct {
	name, substr string
}{
	{PatternDiffTimeout, "could not get diff"},
	{PatternLockNotTaken, "not overriding lock since its age is less than"},
	{PatternCommitSession, "Committing config session due to diffs detected:"},
}

// AgentSnapshot reports the latest tail state for the abort decider.
// LastLineAt is zero before the first line is seen.
type AgentSnapshot struct {
	LastLineAt  time.Time
	MatchCounts map[string]int
}

// AgentTail tails <working-dir>/orchestrator.agent.log via tailer.Tailer
// and appends one NDJSON row per line to observer.agent_log.json. It also
// tracks the latest line timestamp and per-pattern match counts.
type AgentTail struct {
	inPath   string
	outPath  string
	interval time.Duration
	logger   *slog.Logger
	tail     *tailer.Tailer
	now      func() time.Time

	mu          sync.RWMutex
	lastLineAt  time.Time
	matchCounts map[string]int
}

func NewAgent(workingDir string, interval time.Duration, logger *slog.Logger) *AgentTail {
	inPath := filepath.Join(workingDir, agentInputFilename)
	return &AgentTail{
		inPath:      inPath,
		outPath:     filepath.Join(workingDir, agentOutputFilename),
		interval:    interval,
		logger:      logger,
		tail:        tailer.New(inPath),
		now:         time.Now,
		matchCounts: map[string]int{},
	}
}

func (a *AgentTail) Run(ctx context.Context) error {
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	defer a.tail.Close()
	a.tick()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.tick()
		}
	}
}

// Snapshot returns a copy of the current tail state. Safe for concurrent use.
func (a *AgentTail) Snapshot() AgentSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	counts := make(map[string]int, len(a.matchCounts))
	for k, v := range a.matchCounts {
		counts[k] = v
	}
	return AgentSnapshot{LastLineAt: a.lastLineAt, MatchCounts: counts}
}

type agentLogRow struct {
	TNS  int64  `json:"t_ns"`
	Line string `json:"line"`
}

func (a *AgentTail) tick() {
	lines, err := a.tail.Poll()
	if err != nil {
		// ErrOversizeLine is non-fatal: the partial was dropped, but any
		// complete lines surfaced before the overflow are still valid.
		a.logger.Warn("agent log tail failed", "path", a.inPath, "err", err)
		if !errors.Is(err, tailer.ErrOversizeLine) {
			return
		}
	}
	if len(lines) == 0 {
		return
	}
	now := a.now().UTC()
	tNS := now.UnixNano()
	f, err := os.OpenFile(a.outPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		a.logger.Warn("agent log append failed", "path", a.outPath, "err", err)
		return
	}
	defer f.Close()
	a.mu.Lock()
	for _, line := range lines {
		row, err := json.Marshal(agentLogRow{TNS: tNS, Line: line})
		if err != nil {
			continue
		}
		row = append(row, '\n')
		if _, err := f.Write(row); err != nil {
			a.logger.Warn("agent log write failed", "path", a.outPath, "err", err)
			break
		}
		for _, p := range agentPatterns {
			if strings.Contains(line, p.substr) {
				a.matchCounts[p.name]++
			}
		}
	}
	a.lastLineAt = now
	a.mu.Unlock()
}

var _ collector.Collector = (*AgentTail)(nil)
