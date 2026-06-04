// Package format renders Summary / Comparison values into the strings that
// `device-reporter summary` and `device-reporter compare` write to stdout.
// Kept separate from `analyze` so the analyzer stays I/O-free.
package format

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-reporter/pkg/analyze"
	"github.com/malbeclabs/doublezero/tools/stress/device-reporter/pkg/parser"
)

// Summary writes a markdown report for one run to w.
func Summary(w io.Writer, s analyze.Summary, r *parser.Run) {
	bw := &writer{w: w}

	bw.printf("# Stress run summary\n\n")
	if s.RunID != "" {
		bw.printf("**Run ID**: `%s`\n", s.RunID)
	}
	if r.Path != "" {
		bw.printf("**Path**: `%s`\n", r.Path)
	}
	dut := "containerized cEOS"
	if s.IsPhysical {
		dut = "physical EOS"
	}
	bw.printf("**Target**: %s users, batch=%d, hold=%s, dut=%s\n", fmtInt(s.Target), s.Batch, analyze.FormatDurationOrZero(s.Hold), dut)
	if !s.StartedAt.IsZero() {
		bw.printf("**Started**: %s UTC\n", s.StartedAt.UTC().Format("2006-01-02 15:04:05"))
	}
	bw.printf("**Wall clock**: %s\n", fmtDur(s.Duration))
	bw.printf("**Outcome**: **%s**", strings.ToUpper(s.Outcome))
	if s.Outcome == "aborted" {
		bw.printf(" — trigger=`%s` (%s)", s.AbortTrigger, s.AbortDetail)
	}
	bw.printf("\n\n")

	bw.printf("## Onchain\n\n")
	bw.printf("| Phase | Users | Wall time | p50 submit→activate | p95 |\n")
	bw.printf("|---|---|---|---|---|\n")
	bw.printf("| Provision | %s | %s | %s | %s |\n",
		fmtInt(s.OnchainLatencies.ProvisionUsers),
		fmtDur(s.ProvisionDuration),
		fmtDur(s.OnchainLatencies.ProvisionSubmitToActivateP50),
		fmtDur(s.OnchainLatencies.ProvisionSubmitToActivateP95))
	bw.printf("| Deprovision | %s | %s | %s | %s |\n",
		fmtInt(s.OnchainLatencies.DeprovisionUsers),
		fmtDur(s.DeprovisionDuration),
		fmtDur(s.OnchainLatencies.DeprovisionSubmitToActivateP50),
		fmtDur(s.OnchainLatencies.DeprovisionSubmitToActivateP95))
	bw.printf("\n")

	// onchain→on-device is the gap from `activate` (user account written
	// onchain) to `applied` (first commit cycle whose diff includes the
	// user's tunnel interface). Render only when at least one user
	// produced both events so that runs without agent data (`--no-agent`,
	// device-side path broken) don't leave an empty table behind.
	if s.OnchainLatencies.UsersApplied > 0 {
		bw.printf("| Phase | Users on device | p50 onchain→on-device | p95 |\n")
		bw.printf("|---|---|---|---|\n")
		bw.printf("| Provision | %s | %s | %s |\n",
			fmtInt(s.OnchainLatencies.UsersApplied),
			fmtDur(s.OnchainLatencies.ActivateToAppliedP50),
			fmtDur(s.OnchainLatencies.ActivateToAppliedP95))
		bw.printf("\n")
		// Linearity hint: does the gap grow as the run progresses?
		// `+X ms / user` means each additional active user pushes the
		// next user's onchain→on-device by that much.
		writeFit(bw, "active users", s.OnchainToOnDeviceFit, time.Millisecond, "ms/user")
		bw.printf("\n")
	}

	bw.printf("**Event counts**: ")
	keys := []string{"submit", "confirm", "activate", "deprovision_submit", "deprovision_confirm", "deprovision_activate", "pre_commit_log", "applied"}
	var parts []string
	for _, k := range keys {
		if v, ok := s.EventCounts[k]; ok && v > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", k, v))
		}
	}
	bw.printf("%s\n\n", strings.Join(parts, " · "))

	if s.AgentCommitStats.Cycles > 0 {
		bw.printf("## Agent\n\n")
		st := s.AgentCommitStats
		bw.printf("- Cycles: %d (commits=%d, internal aborts=%d, unfinished=%d)\n",
			st.Cycles, st.CommitCount, st.AbortCount, st.UnfinishedCount)
		bw.printf("- Avg commit duration: **%s**\n", fmtDur(st.AvgCommitDuration))
		bw.printf("- Max config received: **%s lines / %s bytes**\n", fmtInt(st.MaxLines), fmtInt(st.MaxBytes))
		bw.printf("\n")

		// Per-cycle breakdown: one row per agent commit cycle, joined
		// with the runlog so each row knows how many users it pushed and
		// the within-cycle onchain→on-device lag distribution. Skip when
		// the join produced nothing useful (e.g. --no-agent runs where
		// the cycle list is empty already).
		if len(s.CommitCycles) > 0 {
			bw.printf("### Per-cycle wall time\n\n")
			bw.printf("| Cycle | Users (cycle) | Users (cumulative) | Lines | Bytes | Commit time | p50 onchain→on-device | Max |\n")
			bw.printf("|---:|---:|---:|---:|---:|---:|---:|---:|\n")
			cumulative := 0
			for i, c := range s.CommitCycles {
				cumulative += c.UsersCommitted
				p50, maxv := "—", "—"
				if c.OnchainToOnDeviceMax > 0 {
					p50 = fmtDur(c.OnchainToOnDeviceP50)
					maxv = fmtDur(c.OnchainToOnDeviceMax)
				}
				bw.printf("| %d | %s | %s | %s | %s | %s | %s | %s |\n",
					i+1,
					fmtInt(c.UsersCommitted),
					fmtInt(cumulative),
					fmtInt(c.ReceivedLines),
					fmtInt(c.ReceivedBytes),
					fmtDur(c.CommitDuration),
					p50, maxv)
			}
			bw.printf("\n")
		}

		bw.printf("### Commit duration vs config size\n\n")
		writeFit(bw, "bytes", s.CommitVsBytes, time.Microsecond, "µs/byte")
		writeFit(bw, "lines", s.CommitVsLines, time.Microsecond, "µs/line")
		bw.printf("\n")
	}

	if len(s.AgentErrorTopK) > 0 {
		bw.printf("## Agent CLI errors (top patterns)\n\n")
		bw.printf("| Count | Normalized command :: reason |\n|---|---|\n")
		for _, e := range s.AgentErrorTopK {
			bw.printf("| %d | `%s` |\n", e.Count, e.NormalizedCommand)
		}
		bw.printf("\n")
	}

	if s.Outcome == "aborted" {
		bw.printf("## Abort\n\n")
		bw.printf("- **Reason**: `%s`\n", s.AbortReason)
		bw.printf("- **Trigger**: `%s`\n", s.AbortTrigger)
		bw.printf("- **Detail**: %s\n", s.AbortDetail)
		bw.printf("\n")
	}
}

// writeFit prints a one-line summary of a LinearFit. The slope is
// re-expressed in the unit the reader actually wants (µs per byte, etc.),
// and R² is rendered as a 0-1 number plus a verdict.
func writeFit(bw *writer, axis string, f analyze.LinearFit, unit time.Duration, slopeUnit string) {
	if f.N < 2 {
		bw.printf("- vs %s: not enough data (n=%d)\n", axis, f.N)
		return
	}
	// Slope is duration/unit-of-x in nanoseconds, convert to caller units.
	slopeInUnit := f.Slope / float64(unit)
	verdict := classifyR2(f.R2)
	bw.printf("- vs **%s**: slope = **%.2f %s**, R² = **%.3f** (%s, n=%d)\n",
		axis, slopeInUnit, slopeUnit, f.R2, verdict, f.N)
}

func classifyR2(r2 float64) string {
	if math.IsNaN(r2) {
		return "indeterminate"
	}
	switch {
	case r2 >= 0.95:
		return "essentially linear"
	case r2 >= 0.85:
		return "roughly linear"
	case r2 >= 0.6:
		return "loosely linear"
	default:
		return "not well-fit by a line"
	}
}

// Comparison writes a side-by-side markdown table for two runs.
func Comparison(w io.Writer, c analyze.Comparison) {
	bw := &writer{w: w}
	bw.printf("# Stress run comparison\n\n")
	if c.A != nil && c.A.RunID != "" {
		bw.printf("- **A**: `%s` (target=%d, batch=%d)\n", c.A.RunID, c.A.Target, c.A.Batch)
	}
	if c.B != nil && c.B.RunID != "" {
		bw.printf("- **B**: `%s` (target=%d, batch=%d)\n", c.B.RunID, c.B.Target, c.B.Batch)
	}
	bw.printf("\n| Metric | A | B | Δ |\n|---|---|---|---|\n")
	for _, row := range c.Rows {
		delta := "—"
		if row.PctMeaningful {
			delta = fmt.Sprintf("%+.1f%%", row.DeltaPct)
		}
		bw.printf("| %s | %s | %s | %s |\n", row.Label, row.AValue, row.BValue, delta)
	}
	bw.printf("\n")
}

// writer wraps an io.Writer with a printf shortcut that swallows the error
// (the markdown writer outputs to stdout / a file the caller owns; a write
// error is reported when the caller calls Sync/Close on it).
type writer struct{ w io.Writer }

func (w *writer) printf(f string, a ...any) { fmt.Fprintf(w.w, f, a...) }

// fmtInt / fmtDur are thin wrappers around the exported analyze helpers
// so the markdown writer doesn't duplicate their definitions. Kept
// unexported here so call sites read naturally inside this file.
func fmtInt(n int) string           { return analyze.FormatInt(n) }
func fmtDur(d time.Duration) string { return analyze.FormatDuration(d) }
