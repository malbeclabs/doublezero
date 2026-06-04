package format

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-reporter/pkg/parser"
)

// ExportMetric names a column-set the CLI's `export` subcommand can write.
type ExportMetric string

const (
	// MetricCommitLatency emits one row per agent commit cycle:
	// run_id,outcome,received_at_ns,received_lines,received_bytes,commit_started_at_ns,finalized_at_ns,commit_duration_ns
	MetricCommitLatency ExportMetric = "commit-latency"
	// MetricRunlog emits one row per orchestrator-runlog event:
	// run_id,event,user_index,tunnel_id,t_ns,n_after_event
	MetricRunlog ExportMetric = "runlog"
)

// ExportCSV writes `metric` rows from r to w. Flush failures (e.g. broken
// pipe on the receiving side) are surfaced via the returned error.
func ExportCSV(w io.Writer, r *parser.Run, metric ExportMetric) error {
	cw := csv.NewWriter(w)
	var werr error
	switch metric {
	case MetricCommitLatency:
		werr = writeCommitLatency(cw, r)
	case MetricRunlog:
		werr = writeRunlog(cw, r)
	default:
		return fmt.Errorf("unknown metric %q (known: %s, %s)", metric, MetricCommitLatency, MetricRunlog)
	}
	cw.Flush()
	if werr != nil {
		return werr
	}
	return cw.Error()
}

// nsOrEmpty renders a time.Time as its UnixNano epoch, or "" when the time
// is zero. Without this, unfinished cycles (zero ReceivedAt / FinalizedAt)
// serialize as -6795364578871345152 — the int64 min that UnixNano returns
// for the zero value — which poisons spreadsheet / pandas analysis.
func nsOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d", t.UnixNano())
}

func writeCommitLatency(cw *csv.Writer, r *parser.Run) error {
	runID := ""
	if r.Config != nil {
		runID = r.Config.RunID
	}
	if err := cw.Write([]string{
		"run_id", "outcome",
		"received_at_ns", "received_lines", "received_bytes",
		"commit_started_at_ns", "finalized_at_ns", "commit_duration_ns",
	}); err != nil {
		return err
	}
	// Stable ordering by CommitStartedAt. The parser already produces cycles
	// in agent-log order, but be explicit so a future re-parse can't
	// accidentally shuffle.
	sorted := append([]parser.AgentCycle(nil), r.Cycles...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CommitStartedAt.Before(sorted[j].CommitStartedAt)
	})
	for _, c := range sorted {
		err := cw.Write([]string{
			runID,
			c.Outcome,
			nsOrEmpty(c.ReceivedAt),
			fmt.Sprintf("%d", c.ReceivedLines),
			fmt.Sprintf("%d", c.ReceivedBytes),
			nsOrEmpty(c.CommitStartedAt),
			nsOrEmpty(c.FinalizedAt),
			fmt.Sprintf("%d", c.CommitDuration().Nanoseconds()),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func writeRunlog(cw *csv.Writer, r *parser.Run) error {
	runID := ""
	if r.Config != nil {
		runID = r.Config.RunID
	}
	if err := cw.Write([]string{
		"run_id", "event", "user_index", "tunnel_id", "t_ns", "n_after_event",
	}); err != nil {
		return err
	}
	for _, e := range r.Events {
		err := cw.Write([]string{
			runID,
			e.Event,
			fmt.Sprintf("%d", e.UserIndex),
			fmt.Sprintf("%d", e.TunnelID),
			fmt.Sprintf("%d", e.TNs),
			fmt.Sprintf("%d", e.NAfterEvent),
		})
		if err != nil {
			return err
		}
	}
	return nil
}
