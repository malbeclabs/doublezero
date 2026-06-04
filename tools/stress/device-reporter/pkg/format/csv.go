package format

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"

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

// ExportCSV writes `metric` rows from r to w.
func ExportCSV(w io.Writer, r *parser.Run, metric ExportMetric) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()
	switch metric {
	case MetricCommitLatency:
		return writeCommitLatency(cw, r)
	case MetricRunlog:
		return writeRunlog(cw, r)
	default:
		return fmt.Errorf("unknown metric %q (known: %s, %s)", metric, MetricCommitLatency, MetricRunlog)
	}
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
	for _, c := range r.Cycles {
		// Stable ordering by CommitStartedAt — the parser already produces
		// cycles in agent-log order, but be explicit so future re-parses
		// can't accidentally shuffle.
		_ = c
	}
	sorted := append([]parser.AgentCycle(nil), r.Cycles...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CommitStartedAt.Before(sorted[j].CommitStartedAt)
	})
	for _, c := range sorted {
		err := cw.Write([]string{
			runID,
			c.Outcome,
			fmt.Sprintf("%d", c.ReceivedAt.UnixNano()),
			fmt.Sprintf("%d", c.ReceivedLines),
			fmt.Sprintf("%d", c.ReceivedBytes),
			fmt.Sprintf("%d", c.CommitStartedAt.UnixNano()),
			fmt.Sprintf("%d", c.FinalizedAt.UnixNano()),
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
