package analyze

import (
	"sort"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-reporter/pkg/parser"
)

// Summary is the rolled-up view of a run that the markdown writer renders.
// Every field is derived from the Run, so the summary itself stays a
// dumb data carrier (easier to test against, easier to extend).
type Summary struct {
	// RunID, taken straight from orchestrator-config.json.
	RunID string
	// IsPhysical is true when the agent flags indicate a non-containerized
	// DUT (orchestrator passes prefix/pubkey directly).
	IsPhysical bool
	// Target / Batch / Hold are the headline knobs from the config.
	Target int
	Batch  int
	Hold   time.Duration
	// StartedAt / EndedAt / Duration are pulled from runlog timestamps.
	StartedAt time.Time
	EndedAt   time.Time
	Duration  time.Duration
	// Outcome is one of "success", "aborted", "unfinished".
	Outcome      string
	AbortReason  string // populated when Outcome == "aborted"
	AbortTrigger string
	AbortDetail  string

	// EventCounts is the per-event-type tally from the runlog.
	EventCounts parser.EventCounts

	// ProvisionDuration is the wall time from the first `submit` to the
	// last `activate`. DeprovisionDuration mirrors for the deprovision phase.
	ProvisionDuration   time.Duration
	DeprovisionDuration time.Duration

	// OnchainLatencies bundles per-user submit→activate stats for both phases.
	OnchainLatencies OnchainLatencies

	// AgentCommitStats summarizes the agent-side commit cycles.
	AgentCommitStats AgentCommitStats

	// CommitVsBytes / CommitVsLines fit commit duration against the
	// received-config size. Empty fits (N < 2) indicate not enough cycles.
	CommitVsBytes LinearFit
	CommitVsLines LinearFit

	// AgentErrorTopK is up to k=8 most common CLI failure patterns
	// (command-text with tunnel numbers normalized).
	AgentErrorTopK []AgentErrorBucket
}

// OnchainLatencies collects per-user submit→activate timing stats for both
// the provision and deprovision phases, plus the activate→applied gap that
// measures how long it took for the agent to actually configure each
// user's tunnel interface after the user account was activated onchain
// (provision-only — deprovision has no `applied` event).
type OnchainLatencies struct {
	ProvisionSubmitToActivateP50   time.Duration
	ProvisionSubmitToActivateP95   time.Duration
	DeprovisionSubmitToActivateP50 time.Duration
	DeprovisionSubmitToActivateP95 time.Duration
	ProvisionUsers                 int
	DeprovisionUsers               int

	// ActivateToAppliedP50 / P95 are the per-user gap from `activate` to
	// `applied` events in the provision phase. UsersApplied is the count
	// of provision users that had both events (a user is dropped from the
	// percentile inputs if either side is missing).
	ActivateToAppliedP50 time.Duration
	ActivateToAppliedP95 time.Duration
	UsersApplied         int
}

// AgentCommitStats summarizes the agent-cycle table.
type AgentCommitStats struct {
	Cycles            int
	CommitCount       int // Outcome == "commit"
	AbortCount        int // Outcome == "abort" (commit-internal abort, NOT observer sentinel)
	UnfinishedCount   int // Cycle that never finalized (agent killed mid-commit)
	MaxLines          int
	MaxBytes          int
	AvgCommitDuration time.Duration
}

// AgentErrorBucket groups CLI-command failures by normalized command text.
type AgentErrorBucket struct {
	NormalizedCommand string
	Count             int
}

// BuildSummary is the top-level analysis entry point.
func BuildSummary(r *parser.Run) Summary {
	s := Summary{
		EventCounts: parser.CountEvents(r.Events),
	}
	if r.Config != nil {
		s.RunID = r.Config.RunID
		s.IsPhysical = r.Config.IsPhysical()
		s.Target = r.Config.TargetUserCount
		s.Batch = r.Config.UsersPerBatch
		s.Hold = time.Duration(r.Config.HoldSeconds) * time.Second
	}
	if start, ok := r.StartedAt(); ok {
		s.StartedAt = start
	}
	if end, ok := r.EndedAt(); ok {
		s.EndedAt = end
	}
	s.Duration = r.Duration()

	// Phase durations: submit → activate. We can be precise because each
	// phase emits a final activate (or deprovision_activate) that closes it.
	if first := firstEvent(r.Events, "submit"); first != nil {
		if last := lastEvent(r.Events, "activate"); last != nil {
			s.ProvisionDuration = last.Time().Sub(first.Time())
		}
	}
	if first := firstEvent(r.Events, "deprovision_submit"); first != nil {
		if last := lastEvent(r.Events, "deprovision_activate"); last != nil {
			s.DeprovisionDuration = last.Time().Sub(first.Time())
		}
	}

	// Outcome detection: an abort sentinel takes precedence; otherwise the
	// run is "success" when every observed submit has a matching activate
	// in both phases AND at least one event of any kind landed (so a run
	// with zero recorded events is "unfinished", not vacuously successful).
	// Guarding on `submit > 0` alone would mark a deprovision-only artifact
	// (a re-run that only tore down a prior run's leftovers) as unfinished
	// even though it converged.
	switch {
	case r.Abort != nil:
		s.Outcome = "aborted"
		s.AbortReason = r.Abort.Reason
		s.AbortTrigger = r.Abort.Trigger
		s.AbortDetail = r.Abort.Detail
	case s.EventCounts["activate"] == s.EventCounts["submit"] &&
		s.EventCounts["deprovision_activate"] == s.EventCounts["deprovision_submit"] &&
		(s.EventCounts["submit"]+s.EventCounts["deprovision_submit"]) > 0:
		s.Outcome = "success"
	default:
		s.Outcome = "unfinished"
	}

	s.OnchainLatencies = onchainLatencies(r.Events)
	s.AgentCommitStats = agentCommitStats(r.Cycles)
	s.CommitVsBytes, s.CommitVsLines = commitVsSizeFits(r.Cycles)
	s.AgentErrorTopK = topAgentErrors(r.CliErrors, 8)

	return s
}

func firstEvent(events []parser.Event, kind string) *parser.Event {
	for i := range events {
		if events[i].Event == kind {
			return &events[i]
		}
	}
	return nil
}

func lastEvent(events []parser.Event, kind string) *parser.Event {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Event == kind {
			return &events[i]
		}
	}
	return nil
}

// onchainLatencies computes per-user submit→activate gaps for both phases
// and the per-user activate→applied gap for the provision phase. Pairs
// are keyed by UserIndex. Missing-side events drop the user from the
// corresponding percentile inputs (a user with `submit` but no `activate`
// counts toward neither submit→activate population, for example).
//
// Activate→applied is provision-only because the agent emits `applied`
// only when a `+ interface Tunnel<N>` shows up in the commit diff — pure
// deprovision diffs (only removals) do not produce `applied` events.
func onchainLatencies(events []parser.Event) OnchainLatencies {
	type key struct {
		phase string
		idx   int
	}
	// Track whether each timestamp was observed explicitly rather than
	// relying on TNs==0 as a "missing" sentinel — Unix epoch 0 is a
	// theoretically valid timestamp and shouldn't be misclassified as
	// absent.
	type pair struct {
		submitTNs, activateTNs int64
		hasSubmit, hasActivate bool
	}
	pairs := map[key]*pair{}
	// applied events are provision-only and keyed by UserIndex alone.
	type appliedPair struct {
		appliedTNs int64
		hasApplied bool
	}
	applieds := map[int]*appliedPair{}
	for _, e := range events {
		if e.Event == "applied" {
			p, ok := applieds[e.UserIndex]
			if !ok {
				p = &appliedPair{}
				applieds[e.UserIndex] = p
			}
			// Take the first `applied` per user: a user can only be
			// configured once per run, and a stray second event (e.g. an
			// agent restart that re-applies the same diff) is the agent
			// recovering, not a new device-creation moment.
			if !p.hasApplied {
				p.appliedTNs = e.TNs
				p.hasApplied = true
			}
			continue
		}
		var phase, role string
		switch e.Event {
		case "submit":
			phase, role = "provision", "submit"
		case "activate":
			phase, role = "provision", "activate"
		case "deprovision_submit":
			phase, role = "deprovision", "submit"
		case "deprovision_activate":
			phase, role = "deprovision", "activate"
		default:
			continue
		}
		k := key{phase, e.UserIndex}
		p, ok := pairs[k]
		if !ok {
			p = &pair{}
			pairs[k] = p
		}
		if role == "submit" {
			p.submitTNs = e.TNs
			p.hasSubmit = true
		} else {
			p.activateTNs = e.TNs
			p.hasActivate = true
		}
	}

	var provGaps, deprovGaps, applyGaps []float64
	for k, p := range pairs {
		if !p.hasSubmit || !p.hasActivate {
			continue
		}
		gap := float64(p.activateTNs - p.submitTNs)
		if k.phase == "provision" {
			provGaps = append(provGaps, gap)
			// activate→applied only makes sense when we know the activate
			// timestamp. If the user also has an applied event, record it.
			if ap, ok := applieds[k.idx]; ok && ap.hasApplied {
				applyGaps = append(applyGaps, float64(ap.appliedTNs-p.activateTNs))
			}
		} else {
			deprovGaps = append(deprovGaps, gap)
		}
	}
	sort.Float64s(provGaps)
	sort.Float64s(deprovGaps)
	sort.Float64s(applyGaps)
	return OnchainLatencies{
		ProvisionSubmitToActivateP50:   time.Duration(Percentile(provGaps, 0.50)),
		ProvisionSubmitToActivateP95:   time.Duration(Percentile(provGaps, 0.95)),
		DeprovisionSubmitToActivateP50: time.Duration(Percentile(deprovGaps, 0.50)),
		DeprovisionSubmitToActivateP95: time.Duration(Percentile(deprovGaps, 0.95)),
		ProvisionUsers:                 len(provGaps),
		DeprovisionUsers:               len(deprovGaps),
		ActivateToAppliedP50:           time.Duration(Percentile(applyGaps, 0.50)),
		ActivateToAppliedP95:           time.Duration(Percentile(applyGaps, 0.95)),
		UsersApplied:                   len(applyGaps),
	}
}

func agentCommitStats(cycles []parser.AgentCycle) AgentCommitStats {
	stats := AgentCommitStats{Cycles: len(cycles)}
	var total time.Duration
	var counted int
	for _, c := range cycles {
		switch c.Outcome {
		case "commit":
			stats.CommitCount++
		case "abort":
			stats.AbortCount++
		case "unfinished":
			stats.UnfinishedCount++
		}
		if c.ReceivedLines > stats.MaxLines {
			stats.MaxLines = c.ReceivedLines
		}
		if c.ReceivedBytes > stats.MaxBytes {
			stats.MaxBytes = c.ReceivedBytes
		}
		if d := c.CommitDuration(); d > 0 {
			total += d
			counted++
		}
	}
	if counted > 0 {
		stats.AvgCommitDuration = total / time.Duration(counted)
	}
	return stats
}

// commitVsSizeFits returns (vs-bytes, vs-lines) linear fits. Only cycles
// that committed successfully AND have a paired Received-line are
// considered — abort/unfinished cycles or commits with no preceding
// Received-pair would skew the slope.
func commitVsSizeFits(cycles []parser.AgentCycle) (LinearFit, LinearFit) {
	var bx, lx, y []float64
	for _, c := range cycles {
		if c.Outcome != "commit" {
			continue
		}
		d := c.CommitDuration()
		if d <= 0 {
			continue
		}
		if c.ReceivedBytes == 0 && c.ReceivedLines == 0 {
			continue
		}
		bx = append(bx, float64(c.ReceivedBytes))
		lx = append(lx, float64(c.ReceivedLines))
		y = append(y, float64(d))
	}
	return LinearLeastSquares(bx, y), LinearLeastSquares(lx, y)
}
