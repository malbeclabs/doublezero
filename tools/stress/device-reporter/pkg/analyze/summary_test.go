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
