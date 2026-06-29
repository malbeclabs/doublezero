package parser

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleAgentLog = `2026/06/04 00:32:01.930732 eapi.go:217: Received 7551 lines of configuration from controller
2026/06/04 00:32:01.930767 eapi.go:218: Received 167755 bytes of configuration from controller
2026/06/04 00:32:15.605538 eapi.go:274: Committing config session due to diffs detected: --- system:/running-config
2026/06/04 00:32:18.124050 eapi.go:295: Configuration session finalized with command 'configure session doublezero-agent-1780533121 commit'
2026/06/04 00:32:18.222712 eapi.go:217: Received 6143 lines of configuration from controller
2026/06/04 00:32:18.222734 eapi.go:218: Received 159258 bytes of configuration from controller
2026/06/04 00:32:28.586218 eapi.go:274: Committing config session due to diffs detected: --- system:/running-config
2026/06/04 00:32:30.618562 eapi.go:295: Configuration session finalized with command 'configure session doublezero-agent-1780533138 commit'
2026/06/04 00:32:35.000000 eapi.go:139: ERROR: pollAndConfigureDevice returned error running eAPI cmd 'CLI command 67 of 253 ' interface Tunnel500' failed: invalid command' at line 1
`

func TestLoadAgentLog_PairsReceivedAndFinalized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	if err := os.WriteFile(path, []byte(sampleAgentLog), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	cycles, errs, err := loadAgentLog(path)
	if err != nil {
		t.Fatalf("loadAgentLog: %v", err)
	}
	if len(cycles) != 2 {
		t.Fatalf("expected 2 cycles, got %d (%+v)", len(cycles), cycles)
	}
	// Cycle 0 should pair the 7551/167755 receive with the first commit.
	c0 := cycles[0]
	if c0.ReceivedLines != 7551 || c0.ReceivedBytes != 167755 {
		t.Errorf("cycle 0 receive sizes wrong: lines=%d bytes=%d", c0.ReceivedLines, c0.ReceivedBytes)
	}
	if c0.Outcome != "commit" {
		t.Errorf("cycle 0 outcome should be commit, got %q", c0.Outcome)
	}
	if c0.CommitDuration() == 0 {
		t.Error("cycle 0 commit duration should be > 0")
	}
	// Cycle 1 should pair the 6143/159258 receive with the second commit.
	c1 := cycles[1]
	if c1.ReceivedLines != 6143 || c1.ReceivedBytes != 159258 {
		t.Errorf("cycle 1 receive sizes wrong: lines=%d bytes=%d", c1.ReceivedLines, c1.ReceivedBytes)
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 CLI error, got %d", len(errs))
	}
	e := errs[0]
	if e.Command != " interface Tunnel500" { // intentional leading space — preserved from EOS log shape
		t.Errorf("CLI error command wrong: %q", e.Command)
	}
	if e.Reason != "invalid command" {
		t.Errorf("CLI error reason wrong: %q", e.Reason)
	}
}

func TestLoadAgentLog_UnfinishedCycle(t *testing.T) {
	// Committing without a finalized line → "unfinished" cycle.
	input := `2026/06/04 00:32:01.930732 eapi.go:217: Received 100 lines of configuration from controller
2026/06/04 00:32:01.930767 eapi.go:218: Received 2000 bytes of configuration from controller
2026/06/04 00:32:15.605538 eapi.go:274: Committing config session due to diffs detected: --- system:/running-config
`
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cycles, _, err := loadAgentLog(path)
	if err != nil {
		t.Fatalf("loadAgentLog: %v", err)
	}
	if len(cycles) != 1 || cycles[0].Outcome != "unfinished" {
		t.Fatalf("expected one unfinished cycle, got %+v", cycles)
	}
}

func TestParseAgentTime(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"2026/06/04 00:32:01.930732 eapi.go:217: foo", true},
		{"not a timestamp", false},
		{"2026/06/04 00:32:01 eapi.go:217: missing fractional", false}, // no `.` after seconds
	}
	for _, tc := range cases {
		_, ok := parseAgentTime(tc.in)
		if ok != tc.ok {
			t.Errorf("parseAgentTime(%q) ok=%v, want %v", tc.in, ok, tc.ok)
		}
	}
}
