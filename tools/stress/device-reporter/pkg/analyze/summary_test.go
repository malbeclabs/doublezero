package analyze

import (
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-reporter/pkg/parser"
)

// TestOnchainLatencies_ActivateToApplied exercises the new metric: per-user
// activate→applied gap (the "user account onchain → tunnel interface on
// device" delay). The fixture includes:
//
//   - user 0: complete submit/activate/applied → contributes to all gaps
//   - user 1: submit/activate but no applied (e.g. agent ran behind) →
//     contributes to submit→activate but not activate→applied
//   - user 2: submit/activate/applied → contributes to all gaps
//
// We assert UsersApplied counts only users with both activate AND applied,
// and that the percentile inputs reflect just those users' gaps.
func TestOnchainLatencies_ActivateToApplied(t *testing.T) {
	// Times chosen so the activate→applied gap is 2s for user 0 and 4s for
	// user 2 (and absent for user 1). p50 of {2s,4s} = 4s (the upper-half
	// of the two-point sample under the Percentile helper); we only check
	// p50 <= p95 and that both are in the {2s,4s} range to stay
	// implementation-agnostic about percentile-interpolation choice.
	base := time.Unix(1_700_000_000, 0).UnixNano()
	events := []parser.Event{
		{UserIndex: 0, Event: "submit", TNs: base},
		{UserIndex: 0, Event: "activate", TNs: base + int64(1*time.Second)},
		{UserIndex: 0, Event: "applied", TNs: base + int64(3*time.Second)}, // gap 2s
		{UserIndex: 1, Event: "submit", TNs: base + int64(1*time.Second)},
		{UserIndex: 1, Event: "activate", TNs: base + int64(2*time.Second)},
		// user 1 deliberately has no `applied` event.
		{UserIndex: 2, Event: "submit", TNs: base + int64(2*time.Second)},
		{UserIndex: 2, Event: "activate", TNs: base + int64(3*time.Second)},
		{UserIndex: 2, Event: "applied", TNs: base + int64(7*time.Second)}, // gap 4s
	}

	got := onchainLatencies(events)
	if got.ProvisionUsers != 3 {
		t.Errorf("ProvisionUsers = %d, want 3", got.ProvisionUsers)
	}
	if got.UsersApplied != 2 {
		t.Errorf("UsersApplied = %d, want 2 (user 1 has no applied event)", got.UsersApplied)
	}
	if got.ActivateToAppliedP50 < 2*time.Second || got.ActivateToAppliedP50 > 4*time.Second {
		t.Errorf("ActivateToAppliedP50 = %s, want in [2s, 4s]", got.ActivateToAppliedP50)
	}
	if got.ActivateToAppliedP95 < got.ActivateToAppliedP50 {
		t.Errorf("p95 (%s) must be >= p50 (%s)", got.ActivateToAppliedP95, got.ActivateToAppliedP50)
	}
	if got.ActivateToAppliedP95 > 4*time.Second {
		t.Errorf("ActivateToAppliedP95 = %s, want <= 4s", got.ActivateToAppliedP95)
	}
}

// TestOnchainLatencies_NoAppliedEvents covers the --no-agent / pre-fix-run
// case where no `applied` event ever fires. UsersApplied should be 0 and
// the latency fields should stay at their zero values (the markdown writer
// uses UsersApplied > 0 as the predicate for rendering the new table).
func TestOnchainLatencies_NoAppliedEvents(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UnixNano()
	events := []parser.Event{
		{UserIndex: 0, Event: "submit", TNs: base},
		{UserIndex: 0, Event: "activate", TNs: base + int64(time.Second)},
		{UserIndex: 1, Event: "submit", TNs: base},
		{UserIndex: 1, Event: "activate", TNs: base + int64(time.Second)},
	}
	got := onchainLatencies(events)
	if got.UsersApplied != 0 {
		t.Errorf("UsersApplied = %d, want 0", got.UsersApplied)
	}
	if got.ActivateToAppliedP50 != 0 || got.ActivateToAppliedP95 != 0 {
		t.Errorf("activate→applied percentiles must be zero when no applied events present (got p50=%s p95=%s)",
			got.ActivateToAppliedP50, got.ActivateToAppliedP95)
	}
}

// TestOnchainLatencies_AppliedWithoutActivate guards against a malformed
// runlog (an `applied` arriving for a user_index that has no `activate`
// row). The user must not contribute to activate→applied since there's
// no baseline timestamp to subtract from.
func TestOnchainLatencies_AppliedWithoutActivate(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UnixNano()
	events := []parser.Event{
		{UserIndex: 0, Event: "applied", TNs: base + int64(3*time.Second)},
	}
	got := onchainLatencies(events)
	if got.UsersApplied != 0 {
		t.Errorf("UsersApplied = %d, want 0 (applied without activate must be ignored)", got.UsersApplied)
	}
}

// TestCommitCycles_DiffCheckDuration verifies that the gap from
// `Received N bytes ...` to `Committing config session ...` is computed
// and emitted on each CommitCycle row.
func TestCommitCycles_DiffCheckDuration(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	cycles := []parser.AgentCycle{
		{
			ReceivedAt:      base,
			ReceivedLines:   100,
			ReceivedBytes:   2000,
			CommitStartedAt: base.Add(12 * time.Second),
			FinalizedAt:     base.Add(14 * time.Second),
			Outcome:         "commit",
		},
		{
			// Mid-cycle agent restart: no paired Received line before commit.
			CommitStartedAt: base.Add(20 * time.Second),
			FinalizedAt:     base.Add(21 * time.Second),
			Outcome:         "commit",
		},
	}
	rows := commitCycles(cycles, nil)
	if got, want := rows[0].DiffCheckDuration, 12*time.Second; got != want {
		t.Errorf("cycle 1 DiffCheckDuration = %s, want %s", got, want)
	}
	if rows[1].DiffCheckDuration != 0 {
		t.Errorf("cycle 2 DiffCheckDuration = %s, want 0 (no Received pair)", rows[1].DiffCheckDuration)
	}
}

// TestCommitCycles_JoinsAppliedEventsByTNs verifies that the agent-log
// cycle list and the runlog `applied` events are paired correctly:
//
//   - cycles are listed chronologically by FinalizedAt;
//   - applied events sharing a TNs collapse to one "users committed"
//     count for the matching cycle;
//   - non-commit cycles (abort, unfinished) get zero users and don't
//     consume an applied bucket (which would shift later cycles).
func TestCommitCycles_JoinsAppliedEventsByTNs(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	// Three agent cycles, finalized at base+10s, base+20s, base+30s.
	cycles := []parser.AgentCycle{
		{
			CommitStartedAt: base.Add(8 * time.Second),
			FinalizedAt:     base.Add(10 * time.Second),
			ReceivedLines:   100,
			ReceivedBytes:   2000,
			Outcome:         "commit",
		},
		{
			CommitStartedAt: base.Add(18 * time.Second),
			FinalizedAt:     base.Add(20 * time.Second),
			ReceivedLines:   150,
			ReceivedBytes:   3500,
			Outcome:         "abort", // mid-run abort, no applied events
		},
		{
			CommitStartedAt: base.Add(28 * time.Second),
			FinalizedAt:     base.Add(30 * time.Second),
			ReceivedLines:   200,
			ReceivedBytes:   5000,
			Outcome:         "commit",
		},
	}
	// Cycle 1: two users activated at base+0s, base+5s; both applied at
	// base+10s. Gaps: 10s, 5s. Cycle 3: one user activated at base+25s,
	// applied at base+30s. Gap: 5s.
	events := []parser.Event{
		{UserIndex: 0, Event: "activate", TNs: base.UnixNano()},
		{UserIndex: 1, Event: "activate", TNs: base.Add(5 * time.Second).UnixNano()},
		{UserIndex: 0, Event: "applied", TNs: base.Add(10 * time.Second).UnixNano()},
		{UserIndex: 1, Event: "applied", TNs: base.Add(10 * time.Second).UnixNano()},
		{UserIndex: 2, Event: "activate", TNs: base.Add(25 * time.Second).UnixNano()},
		{UserIndex: 2, Event: "applied", TNs: base.Add(30 * time.Second).UnixNano()},
	}

	rows := commitCycles(cycles, events)
	if len(rows) != 3 {
		t.Fatalf("want 3 rows (one per cycle, including the abort), got %d", len(rows))
	}
	// Cycle 1: 2 users, max gap = 10s.
	if rows[0].UsersCommitted != 2 {
		t.Errorf("cycle 1 UsersCommitted = %d, want 2", rows[0].UsersCommitted)
	}
	if rows[0].OnchainToOnDeviceMax != 10*time.Second {
		t.Errorf("cycle 1 max gap = %s, want 10s", rows[0].OnchainToOnDeviceMax)
	}
	// Cycle 2: abort, must not consume the cycle-3 applied bucket.
	if rows[1].UsersCommitted != 0 {
		t.Errorf("cycle 2 (abort) UsersCommitted = %d, want 0 (abort cycles emit no applied events)", rows[1].UsersCommitted)
	}
	if rows[1].OnchainToOnDeviceMax != 0 {
		t.Errorf("cycle 2 max gap should be zero, got %s", rows[1].OnchainToOnDeviceMax)
	}
	// Cycle 3: 1 user, gap 5s.
	if rows[2].UsersCommitted != 1 {
		t.Errorf("cycle 3 UsersCommitted = %d, want 1 (abort cycle must not have eaten this bucket)", rows[2].UsersCommitted)
	}
	if rows[2].OnchainToOnDeviceMax != 5*time.Second {
		t.Errorf("cycle 3 max gap = %s, want 5s", rows[2].OnchainToOnDeviceMax)
	}
}

// TestOnchainToOnDeviceFit_GrowsWithActiveUsers feeds a synthetic
// dataset where the gap scales linearly with the active-user count and
// asserts the slope comes back positive and R² is near 1.
func TestOnchainToOnDeviceFit_GrowsWithActiveUsers(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UnixNano()
	var events []parser.Event
	// gap_ns = NAfterEvent * 1ms exactly — perfect linearity.
	for i := 1; i <= 10; i++ {
		activateT := base + int64(i)*int64(time.Second)
		gap := int64(i) * int64(time.Millisecond)
		events = append(events,
			parser.Event{UserIndex: i, Event: "activate", TNs: activateT, NAfterEvent: i},
			parser.Event{UserIndex: i, Event: "applied", TNs: activateT + gap},
		)
	}
	fit := onchainToOnDeviceFit(events)
	if fit.N != 10 {
		t.Fatalf("fit.N = %d, want 10", fit.N)
	}
	// Slope is duration (ns) per +1 active user. We expect 1ms = 1e6 ns.
	const wantSlope = float64(time.Millisecond)
	if rel := abs((fit.Slope - wantSlope) / wantSlope); rel > 0.001 {
		t.Errorf("fit.Slope = %v, want ≈ %v (rel err %v)", fit.Slope, wantSlope, rel)
	}
	if fit.R2 < 0.999 {
		t.Errorf("fit.R2 = %v, want near 1.0 (perfectly linear input)", fit.R2)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
