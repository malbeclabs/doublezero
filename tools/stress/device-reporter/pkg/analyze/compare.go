package analyze

import (
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-reporter/pkg/parser"
)

// Comparison holds side-by-side metrics for two runs (A and B), with
// per-row delta values where the metric is numeric. Used by
// `device-reporter compare`.
type Comparison struct {
	A, B *Summary

	// Rows are the comparable metrics, in display order.
	Rows []ComparisonRow
}

// ComparisonRow is one metric in the side-by-side. AValue / BValue are
// human-readable strings (the comparison knows how to format durations,
// ints, percentages). DeltaPct is (B-A)/A * 100; PctMeaningful is false
// when A is zero (so the markdown writer can show "—" instead of inf%).
type ComparisonRow struct {
	Label         string
	AValue        string
	BValue        string
	DeltaPct      float64
	PctMeaningful bool
}

// BuildComparison computes the comparable metric rows between two runs.
func BuildComparison(a, b *parser.Run) Comparison {
	sa := BuildSummary(a)
	sb := BuildSummary(b)
	c := Comparison{A: &sa, B: &sb}

	add := func(label, av, bv string, da, db float64) {
		row := ComparisonRow{Label: label, AValue: av, BValue: bv}
		if da > 0 {
			row.DeltaPct = (db - da) / da * 100
			row.PctMeaningful = true
		}
		c.Rows = append(c.Rows, row)
	}

	addInt := func(label string, av, bv int) {
		add(label, fmtInt(av), fmtInt(bv), float64(av), float64(bv))
	}
	addDur := func(label string, av, bv time.Duration) {
		add(label, fmtDur(av), fmtDur(bv), float64(av), float64(bv))
	}

	addInt("Target users", sa.Target, sb.Target)
	addInt("Batch size", sa.Batch, sb.Batch)
	addDur("Hold", sa.Hold, sb.Hold)
	add("Outcome", sa.Outcome, sb.Outcome, 0, 0)
	addDur("Total duration", sa.Duration, sb.Duration)
	addDur("Provision phase", sa.ProvisionDuration, sb.ProvisionDuration)
	addDur("Deprovision phase", sa.DeprovisionDuration, sb.DeprovisionDuration)
	addDur("Provision p50", sa.OnchainLatencies.ProvisionSubmitToActivateP50, sb.OnchainLatencies.ProvisionSubmitToActivateP50)
	addDur("Provision p95", sa.OnchainLatencies.ProvisionSubmitToActivateP95, sb.OnchainLatencies.ProvisionSubmitToActivateP95)
	addDur("Deprovision p50", sa.OnchainLatencies.DeprovisionSubmitToActivateP50, sb.OnchainLatencies.DeprovisionSubmitToActivateP50)
	addDur("Deprovision p95", sa.OnchainLatencies.DeprovisionSubmitToActivateP95, sb.OnchainLatencies.DeprovisionSubmitToActivateP95)
	addDur("Activate→applied p50", sa.OnchainLatencies.ActivateToAppliedP50, sb.OnchainLatencies.ActivateToAppliedP50)
	addDur("Activate→applied p95", sa.OnchainLatencies.ActivateToAppliedP95, sb.OnchainLatencies.ActivateToAppliedP95)
	addInt("Agent commits", sa.AgentCommitStats.CommitCount, sb.AgentCommitStats.CommitCount)
	addInt("Agent unfinished cycles", sa.AgentCommitStats.UnfinishedCount, sb.AgentCommitStats.UnfinishedCount)
	addDur("Avg commit duration", sa.AgentCommitStats.AvgCommitDuration, sb.AgentCommitStats.AvgCommitDuration)
	addInt("Max config (lines)", sa.AgentCommitStats.MaxLines, sb.AgentCommitStats.MaxLines)
	addInt("Max config (bytes)", sa.AgentCommitStats.MaxBytes, sb.AgentCommitStats.MaxBytes)

	return c
}
