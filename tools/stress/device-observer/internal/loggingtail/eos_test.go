package loggingtail

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"
)

type fakeEOSRunner struct {
	resp atomic.Value // string
	err  atomic.Value // error (nil-safe)
}

func newFakeEOSRunner(initial string) *fakeEOSRunner {
	r := &fakeEOSRunner{}
	r.resp.Store(initial)
	return r
}

func (f *fakeEOSRunner) set(s string)   { f.resp.Store(s) }
func (f *fakeEOSRunner) setErr(e error) { f.err.Store(errorWrap{e}) }
func (f *fakeEOSRunner) RunShowText(cmd string) (string, error) {
	if v, ok := f.err.Load().(errorWrap); ok && v.e != nil {
		return "", v.e
	}
	return f.resp.Load().(string), nil
}

type errorWrap struct{ e error }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func readEOSRows(t *testing.T, path string) []eosLine {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var rows []eosLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r eosLine
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("decode %q: %v", sc.Text(), err)
		}
		rows = append(rows, r)
	}
	return rows
}

// TestParseHappyPath verifies the timestamp + facility/severity capture
// works for default Arista-format syslog lines.
func TestParseHappyPath(t *testing.T) {
	const text = `May 29 12:34:56 dz1 BGP-3-NOTIFICATION: bgp event
May 29 12:34:57 dz1 SYS-6-INFO: hello world
`
	rows := parseEOSLog(text, 12345)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].Facility != "BGP" || rows[0].Severity != "3" {
		t.Errorf("row0 facility/sev = %q/%q, want BGP/3", rows[0].Facility, rows[0].Severity)
	}
	if rows[0].Message != "bgp event" {
		t.Errorf("row0 message = %q, want %q", rows[0].Message, "bgp event")
	}
	if rows[1].Facility != "SYS" || rows[1].Severity != "6" {
		t.Errorf("row1 facility/sev = %q/%q, want SYS/6", rows[1].Facility, rows[1].Severity)
	}
}

// TestParseUnparseable: a line not matching the prefix is still emitted
// with empty severity/facility and the full line in Message.
func TestParseUnparseable(t *testing.T) {
	rows := parseEOSLog("this is not syslog\n", 7)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Severity != "" || rows[0].Facility != "" {
		t.Errorf("unparseable row should have empty severity/facility, got %+v", rows[0])
	}
	if rows[0].Message != "this is not syslog" {
		t.Errorf("message = %q, want full line", rows[0].Message)
	}
}

// TestDedupeAcrossTicks: a line seen on two consecutive ticks is written
// to the output exactly once.
func TestDedupeAcrossTicks(t *testing.T) {
	dir := t.TempDir()
	body := "May 29 12:00:00 dz1 BGP-3-NOTIF: a\nMay 29 12:00:01 dz1 BGP-3-NOTIF: b\n"
	runner := newFakeEOSRunner(body)
	p := NewEOS(runner, dir, time.Second, 30*time.Second, discardLogger())

	p.tick()
	// Same response; overlap window should dedupe.
	p.tick()

	rows := readEOSRows(t, filepath.Join(dir, eosOutputFilename))
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (dedupe failed)", len(rows))
	}
}

// TestNewLineAfterDedupe: a new line appearing on the second tick is
// emitted, the previously-seen lines are not duplicated.
func TestNewLineAfterDedupe(t *testing.T) {
	dir := t.TempDir()
	runner := newFakeEOSRunner("May 29 12:00:00 dz1 BGP-3-NOTIF: a\n")
	p := NewEOS(runner, dir, time.Second, 30*time.Second, discardLogger())
	p.tick()
	runner.set("May 29 12:00:00 dz1 BGP-3-NOTIF: a\nMay 29 12:00:01 dz1 BGP-3-NOTIF: b\n")
	p.tick()

	rows := readEOSRows(t, filepath.Join(dir, eosOutputFilename))
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].Message != "a" || rows[1].Message != "b" {
		t.Errorf("rows = %v, want messages a, b", rows)
	}
}

// TestTickErrorContinues: a per-tick eAPI failure logs WARN and does not
// produce any output, but subsequent successful ticks recover.
func TestTickErrorContinues(t *testing.T) {
	dir := t.TempDir()
	runner := newFakeEOSRunner("May 29 12:00:00 dz1 BGP-3-NOTIF: a\n")
	runner.setErr(errors.New("boom"))
	p := NewEOS(runner, dir, time.Second, 30*time.Second, discardLogger())

	p.tick()
	if _, err := os.Stat(filepath.Join(dir, eosOutputFilename)); !os.IsNotExist(err) {
		t.Fatalf("output file should not exist on failure, stat err = %v", err)
	}
	runner.setErr(nil)
	p.tick()
	rows := readEOSRows(t, filepath.Join(dir, eosOutputFilename))
	if len(rows) != 1 || rows[0].Message != "a" {
		t.Errorf("rows = %v, want [a]", rows)
	}
}

// TestLookbackFloor: a short interval is bumped up to the 30 s floor so
// short --sample-interval runs still have overlap.
func TestLookbackFloor(t *testing.T) {
	dir := t.TempDir()
	runner := newFakeEOSRunner("")
	p := NewEOS(runner, dir, time.Second, 2*time.Second, discardLogger())
	if p.lookback < 30*time.Second {
		t.Fatalf("lookback = %v, want >= 30s", p.lookback)
	}
}

// TestRunCancelsCleanly confirms Run returns nil promptly on ctx cancel.
func TestRunCancelsCleanly(t *testing.T) {
	dir := t.TempDir()
	runner := newFakeEOSRunner("")
	p := NewEOS(runner, dir, time.Hour, 30*time.Second, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
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

// TestNDJSONShape: each row decodes as JSON with the expected fields.
func TestNDJSONShape(t *testing.T) {
	dir := t.TempDir()
	runner := newFakeEOSRunner("May 29 12:00:00 dz1 BGP-3-NOTIF: hello\n")
	p := NewEOS(runner, dir, time.Second, 30*time.Second, discardLogger())
	p.tick()

	body, err := os.ReadFile(filepath.Join(dir, eosOutputFilename))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	line := strings.TrimRight(string(body), "\n")
	var row map[string]any
	if err := json.Unmarshal([]byte(line), &row); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, field := range []string{"t_ns", "time", "severity", "facility", "message"} {
		if _, ok := row[field]; !ok {
			t.Errorf("missing field %q in %v", field, row)
		}
	}
}

var _ collector.Collector = (*EOSPoller)(nil)
