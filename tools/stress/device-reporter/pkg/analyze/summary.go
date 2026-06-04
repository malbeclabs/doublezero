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
	// DUTName is the device-under-test identifier (hostname or IP)
	// extracted from the orchestrator's ssh target, used in the
	// summary header so multiple runs against different DUTs can be
	// told apart at a glance.
	DUTName string
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

	// DiffCheckVsBytes / DiffCheckVsLines fit the per-cycle diff-check
	// time (`Received N bytes` → `Committing config session`) against
	// the received-config size. Diff-check tends to dominate the
	// per-cycle wall time at scale, so the slope here is more
	// load-bearing than the commit-vs-size fits.
	DiffCheckVsBytes LinearFit
	DiffCheckVsLines LinearFit

	// AgentErrorTopK is up to k=8 most common CLI failure patterns
	// (command-text with tunnel numbers normalized).
	AgentErrorTopK []AgentErrorBucket

	// CommitCycles is one row per agent commit cycle, joined against the
	// runlog so each row knows how many users it actually pushed to the
	// device. Used to render the per-cycle wall-time + onchain→on-device
	// lag breakdown. Cycles with no matching `applied` event (e.g. config
	// pushes that contained no user-tunnel additions) are still listed
	// with UsersCommitted == 0 so the operator can see them.
	CommitCycles []CommitCycle

	// OnchainToOnDeviceFit fits the per-user onchain→on-device gap (y, ns)
	// against the active-user count when each user activated (x, count).
	// Slope is duration / +1 user, so the operator can see whether the
	// gap grows as the run progresses. Empty (N < 2) when fewer than two
	// users had both `activate` and `applied`.
	OnchainToOnDeviceFit LinearFit
}

// CommitCycle is the joined view of one agent commit cycle: the agent
// log gives us the lines/bytes/duration; the runlog gives us the count
// of users committed in that cycle plus the within-cycle onchain→on-device
// gap distribution. Rows are ordered by commit-finalized time ascending.
type CommitCycle struct {
	// UsersCommitted is the count of `applied` runlog events that share
	// this cycle's finalize timestamp. Zero for commit cycles that
	// applied no user additions (e.g. controller pushed a no-op delta).
	UsersCommitted int
	ReceivedLines  int
	ReceivedBytes  int
	// DiffCheckDuration is the gap from `Received N bytes of
	// configuration from controller` to `Committing config session due
	// to diffs detected`. It covers the agent's diff-compute +
	// decide-to-commit + configure-session-open time. Zero when the
	// agent log did not emit a paired Received line before the commit
	// (e.g. the agent restarted mid-cycle).
	DiffCheckDuration time.Duration
	// CommitDuration is the gap from `Committing config session ...` to
	// `Configuration session finalized ...`. The wall-clock cost of the
	// commit itself.
	CommitDuration time.Duration
	// OnchainToOnDeviceP50 / Max are within-cycle stats of the gap from
	// each user's `activate` to this commit. They surface the spread —
	// the just-activated user sees a small gap, the oldest user in the
	// batch sees the largest. Both zero when UsersCommitted == 0.
	OnchainToOnDeviceP50 time.Duration
	OnchainToOnDeviceMax time.Duration
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
		s.DUTName = r.Config.DUTName()
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
	s.DiffCheckVsBytes, s.DiffCheckVsLines = diffCheckVsSizeFits(r.Cycles)
	s.AgentErrorTopK = topAgentErrors(r.CliErrors, 8)
	s.CommitCycles = commitCycles(r.Cycles, r.Events)
	s.OnchainToOnDeviceFit = onchainToOnDeviceFit(r.Events)

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

// diffCheckVsSizeFits returns (vs-bytes, vs-lines) linear fits of the
// agent's diff-check time against the received-config size. Same
// inclusion rule as commitVsSizeFits — only successful commits with a
// paired Received line and a positive diff-check gap contribute, so
// agent restarts mid-cycle (zero ReceivedAt) and pure-noop polls don't
// distort the slope.
func diffCheckVsSizeFits(cycles []parser.AgentCycle) (LinearFit, LinearFit) {
	var bx, lx, y []float64
	for _, c := range cycles {
		if c.Outcome != "commit" {
			continue
		}
		if c.ReceivedAt.IsZero() || c.CommitStartedAt.IsZero() {
			continue
		}
		gap := c.CommitStartedAt.Sub(c.ReceivedAt)
		if gap <= 0 {
			continue
		}
		if c.ReceivedBytes == 0 && c.ReceivedLines == 0 {
			continue
		}
		bx = append(bx, float64(c.ReceivedBytes))
		lx = append(lx, float64(c.ReceivedLines))
		y = append(y, float64(gap))
	}
	return LinearLeastSquares(bx, y), LinearLeastSquares(lx, y)
}

// commitCycles joins each agent-log commit cycle with the runlog
// `applied` events that share its finalize timestamp. All `applied`
// events from one commit are emitted by the orchestrator at the same
// instant (the parser stamps them with one wallclock read when the
// "finalized ... commit" line is seen), so grouping the runlog by
// `applied.TNs` yields a clean per-cycle bucket. Buckets are paired
// with cycles in chronological order — the i-th cycle (by FinalizedAt)
// pairs with the i-th distinct applied-TNs.
//
// Cycles whose Outcome is not "commit" (abort, unfinished) are still
// emitted so the operator can see them; their UsersCommitted will be
// zero by construction (the orchestrator doesn't emit Applied on
// abort/unfinished cycles).
func commitCycles(cycles []parser.AgentCycle, events []parser.Event) []CommitCycle {
	if len(cycles) == 0 {
		return nil
	}

	// Build per-user activate timestamps so we can compute the
	// within-cycle onchain→on-device gap for each user.
	activateAt := map[int]int64{}
	for _, e := range events {
		if e.Event == "activate" {
			if _, seen := activateAt[e.UserIndex]; !seen {
				activateAt[e.UserIndex] = e.TNs
			}
		}
	}

	// Group `applied` events by TNs. Events from the same commit cycle
	// share an exact TNs, so a plain map suffices — no fuzzy bucketing.
	type appliedBucket struct {
		tNs   int64
		gaps  []float64 // per-user onchain→on-device gaps (ns)
		count int
	}
	bucketByTNs := map[int64]*appliedBucket{}
	for _, e := range events {
		if e.Event != "applied" {
			continue
		}
		b, ok := bucketByTNs[e.TNs]
		if !ok {
			b = &appliedBucket{tNs: e.TNs}
			bucketByTNs[e.TNs] = b
		}
		b.count++
		if a, hasActivate := activateAt[e.UserIndex]; hasActivate {
			b.gaps = append(b.gaps, float64(e.TNs-a))
		}
	}
	buckets := make([]*appliedBucket, 0, len(bucketByTNs))
	for _, b := range bucketByTNs {
		buckets = append(buckets, b)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].tNs < buckets[j].tNs })

	// Cycles are already in chronological order from the agent-log
	// parser, but make it explicit: sort by FinalizedAt with a stable
	// fallback to CommitStartedAt so unfinished cycles (zero FinalizedAt)
	// trail their finished neighbors instead of jumping to the front.
	sorted := make([]parser.AgentCycle, len(cycles))
	copy(sorted, cycles)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti, tj := sorted[i].FinalizedAt, sorted[j].FinalizedAt
		if ti.IsZero() {
			ti = sorted[i].CommitStartedAt
		}
		if tj.IsZero() {
			tj = sorted[j].CommitStartedAt
		}
		return ti.Before(tj)
	})

	out := make([]CommitCycle, 0, len(sorted))
	bi := 0
	for _, c := range sorted {
		row := CommitCycle{
			ReceivedLines:  c.ReceivedLines,
			ReceivedBytes:  c.ReceivedBytes,
			CommitDuration: c.CommitDuration(),
		}
		// Diff-check is Received → Committing. Skip when either side
		// is missing (agent restart mid-cycle leaves ReceivedAt zero).
		if !c.ReceivedAt.IsZero() && !c.CommitStartedAt.IsZero() {
			if d := c.CommitStartedAt.Sub(c.ReceivedAt); d > 0 {
				row.DiffCheckDuration = d
			}
		}
		// Only successful commits should consume an applied bucket. Abort
		// and unfinished cycles emit no applied events, so giving them
		// a bucket would shift every later cycle's user-count off by one.
		if c.Outcome == "commit" && bi < len(buckets) {
			b := buckets[bi]
			row.UsersCommitted = b.count
			if len(b.gaps) > 0 {
				sort.Float64s(b.gaps)
				row.OnchainToOnDeviceP50 = time.Duration(Percentile(b.gaps, 0.50))
				row.OnchainToOnDeviceMax = time.Duration(b.gaps[len(b.gaps)-1])
			}
			bi++
		}
		out = append(out, row)
	}
	return out
}

// onchainToOnDeviceFit fits the per-user onchain→on-device gap (y) in
// nanoseconds against the count of users active when the user activated
// (x — sourced from the runlog's NAfterEvent column at the activate
// event). The slope tells the operator whether the gap grows as the
// run progresses; R² indicates how linear the trend is.
func onchainToOnDeviceFit(events []parser.Event) LinearFit {
	activate := map[int]parser.Event{}
	applied := map[int]parser.Event{}
	for _, e := range events {
		switch e.Event {
		case "activate":
			if _, seen := activate[e.UserIndex]; !seen {
				activate[e.UserIndex] = e
			}
		case "applied":
			if _, seen := applied[e.UserIndex]; !seen {
				applied[e.UserIndex] = e
			}
		}
	}
	var xs, ys []float64
	for idx, a := range activate {
		ap, ok := applied[idx]
		if !ok {
			continue
		}
		xs = append(xs, float64(a.NAfterEvent))
		ys = append(ys, float64(ap.TNs-a.TNs))
	}
	return LinearLeastSquares(xs, ys)
}
