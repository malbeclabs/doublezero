package loggingtail

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"
)

func appendLog(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readAgentRows(t *testing.T, path string) []agentLogRow {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var rows []agentLogRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r agentLogRow
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("decode: %v", err)
		}
		rows = append(rows, r)
	}
	return rows
}

// TestAgentMissingFileSnapshotZero: before the orchestrator writes the
// agent log, Snapshot's LastLineAt remains zero.
func TestAgentMissingFileSnapshotZero(t *testing.T) {
	dir := t.TempDir()
	a := NewAgent(dir, time.Hour, discardLogger())
	a.tick()
	if !a.Snapshot().LastLineAt.IsZero() {
		t.Errorf("LastLineAt should be zero before any line, got %v", a.Snapshot().LastLineAt)
	}
}

// TestAgentLineAppend: each new line yields an NDJSON row in the output.
func TestAgentLineAppend(t *testing.T) {
	dir := t.TempDir()
	a := NewAgent(dir, time.Hour, discardLogger())
	in := filepath.Join(dir, agentInputFilename)
	appendLog(t, in, "first\nsecond\n")
	a.tick()

	rows := readAgentRows(t, filepath.Join(dir, agentOutputFilename))
	if len(rows) != 2 || rows[0].Line != "first" || rows[1].Line != "second" {
		t.Fatalf("rows = %+v, want [first second]", rows)
	}
	if a.Snapshot().LastLineAt.IsZero() {
		t.Errorf("LastLineAt should be advanced after a line was seen")
	}
}

// TestAgentPatternCounts: each substring is matched and counted; lines
// without any pattern do not bump counters.
func TestAgentPatternCounts(t *testing.T) {
	dir := t.TempDir()
	a := NewAgent(dir, time.Hour, discardLogger())
	in := filepath.Join(dir, agentInputFilename)
	body := "INFO: could not get diff foo timed out after 60 seconds\n" +
		"INFO: Committing config session due to diffs detected: x\n" +
		"INFO: not overriding lock since its age is less than 5m\n" +
		"INFO: unrelated noise\n" +
		"INFO: Committing config session due to diffs detected: y\n"
	appendLog(t, in, body)
	a.tick()

	snap := a.Snapshot()
	if snap.MatchCounts[PatternDiffTimeout] != 1 {
		t.Errorf("diff_timeout = %d, want 1", snap.MatchCounts[PatternDiffTimeout])
	}
	if snap.MatchCounts[PatternLockNotTaken] != 1 {
		t.Errorf("lock_not_taken = %d, want 1", snap.MatchCounts[PatternLockNotTaken])
	}
	if snap.MatchCounts[PatternCommitSession] != 2 {
		t.Errorf("commit_session = %d, want 2", snap.MatchCounts[PatternCommitSession])
	}
}

// TestAgentRotation: a renamed-and-recreated log file is re-tailed from
// the start, lines from both halves are emitted, and the LastLineAt
// advances rather than going backward.
func TestAgentRotation(t *testing.T) {
	dir := t.TempDir()
	a := NewAgent(dir, time.Hour, discardLogger())
	in := filepath.Join(dir, agentInputFilename)
	appendLog(t, in, "before\n")
	a.tick()
	first := a.Snapshot().LastLineAt

	if err := os.Rename(in, in+".1"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	appendLog(t, in, "after\n")
	// Ensure clock advances at least 1 ns so we can detect LastLineAt motion.
	time.Sleep(time.Millisecond)
	a.tick()
	second := a.Snapshot().LastLineAt

	rows := readAgentRows(t, filepath.Join(dir, agentOutputFilename))
	if len(rows) != 2 {
		t.Fatalf("rows after rotation = %d, want 2", len(rows))
	}
	if !second.After(first) {
		t.Errorf("LastLineAt did not advance: first=%v second=%v", first, second)
	}
}

// TestAgentRunCancels: Run returns nil on context cancel.
func TestAgentRunCancels(t *testing.T) {
	dir := t.TempDir()
	a := NewAgent(dir, time.Hour, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s")
	}
}

var _ collector.Collector = (*AgentTail)(nil)
