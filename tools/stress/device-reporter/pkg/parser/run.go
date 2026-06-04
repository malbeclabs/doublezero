// Package parser loads a single stress-test run directory into structured
// data. A run directory is whatever `tools/stress/scripts/run-stress-*.sh`
// drops under `dev/.deploy/.../run/<UTC>/`: the orchestrator's config dump,
// per-event runlog, agent log capture, plus observer artifacts and the abort
// sentinel if the run aborted.
//
// The parser is intentionally lenient: missing files leave the corresponding
// field nil/empty rather than failing the load, since not every run produces
// every artifact (e.g. --no-agent runs have no agent log, runs that didn't
// abort have no sentinel).
package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Run is the in-memory representation of a single run directory.
type Run struct {
	// Path is the absolute path to the run directory on disk.
	Path string
	// Config is the orchestrator's resolved CLI inputs, or nil if the
	// config file is missing.
	Config *OrchestratorConfig
	// Events is every row from orchestrator-runlog.jsonl in file order.
	Events []Event
	// Cycles is the parsed agent commit cycles, one per
	// `Committing config session...` / `Configuration session finalized...`
	// pair. The most-recent received-config sizes are paired in when they
	// precede the commit marker.
	Cycles []AgentCycle
	// CliErrors is every `CLI command X of Y '...' failed: ...` line from
	// the agent log, in file order. These are commit-time validation
	// failures from EOS, not orchestrator-side errors.
	CliErrors []AgentCLIError
	// Abort holds the abort sentinel JSON if it was written, else nil.
	Abort *AbortSentinel
}

// LoadRun reads every artifact present under `dir` into a Run. Missing
// individual artifacts leave the matching field nil/empty; the directory
// itself, however, must exist — a typo'd path is a hard error so callers
// don't silently get an empty Run + a near-empty markdown summary.
// Unparseable artifacts surface as errors so callers can decide whether to
// skip the run or fail.
func LoadRun(dir string) (*Run, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve run dir: %w", err)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("stat run dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", absDir)
	}
	r := &Run{Path: absDir}

	if cfg, err := loadOrchestratorConfig(filepath.Join(absDir, "orchestrator-config.json")); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("orchestrator-config.json: %w", err)
	} else if cfg != nil {
		r.Config = cfg
	}

	if events, err := loadRunlog(filepath.Join(absDir, "orchestrator-runlog.jsonl")); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("orchestrator-runlog.jsonl: %w", err)
	} else {
		r.Events = events
	}

	if cycles, cliErrors, err := loadAgentLog(filepath.Join(absDir, "orchestrator.agent.log")); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("orchestrator.agent.log: %w", err)
	} else {
		r.Cycles = cycles
		r.CliErrors = cliErrors
	}

	if ab, err := loadAbortSentinel(filepath.Join(absDir, "abort")); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("abort: %w", err)
	} else if ab != nil {
		r.Abort = ab
	}

	return r, nil
}

// StartedAt returns the earliest timestamp observed in the runlog, falling
// back to the orchestrator config's RunID timestamp parsing isn't trivial
// (the orchestrator config has no explicit started-at field).
func (r *Run) StartedAt() (time.Time, bool) {
	if len(r.Events) == 0 {
		return time.Time{}, false
	}
	return time.Unix(0, r.Events[0].TNs), true
}

// EndedAt returns the latest timestamp observed in the runlog.
func (r *Run) EndedAt() (time.Time, bool) {
	if len(r.Events) == 0 {
		return time.Time{}, false
	}
	return time.Unix(0, r.Events[len(r.Events)-1].TNs), true
}

// Duration is end - start; zero if either side is missing.
func (r *Run) Duration() time.Duration {
	start, ok1 := r.StartedAt()
	end, ok2 := r.EndedAt()
	if !ok1 || !ok2 {
		return 0
	}
	return end.Sub(start)
}
