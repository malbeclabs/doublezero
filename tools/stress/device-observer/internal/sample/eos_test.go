package sample

import (
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
)

// fakeRunner is a tiny eapiRunner implementation that returns canned
// responses (or errors) per command.
type fakeRunner struct {
	jsonResp map[string]json.RawMessage
	textResp map[string]string
	errs     map[string]error
	calls    atomic.Int32
}

func (f *fakeRunner) RunShowJSON(cmd string) (json.RawMessage, error) {
	f.calls.Add(1)
	if err, ok := f.errs[cmd]; ok {
		return nil, err
	}
	if v, ok := f.jsonResp[cmd]; ok {
		return v, nil
	}
	return json.RawMessage(`{}`), nil
}

func (f *fakeRunner) RunShowText(cmd string) (string, error) {
	f.calls.Add(1)
	if err, ok := f.errs[cmd]; ok {
		return "", err
	}
	if v, ok := f.textResp[cmd]; ok {
		return v, nil
	}
	return "", nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newSamplerWithClock(t *testing.T, runner eapiRunner, dir string, clock func() time.Time) *Sampler {
	t.Helper()
	s := NewSampler(runner, dir, time.Hour, discardLogger())
	s.now = clock
	return s
}

// TestTickWritesAllFiles verifies a single tick produces one file per
// command with the expected name prefix and body.
func TestTickWritesAllFiles(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{
		jsonResp: map[string]json.RawMessage{
			"show hardware capacity":  json.RawMessage(`{"capacity":1}`),
			"show gre tunnel static":  json.RawMessage(`{"tunnels":[]}`),
			"show processes top once": json.RawMessage(`{"processes":[]}`),
		},
		textResp: map[string]string{
			"show logging errors":   "errlog\n",
			"show logging critical": "critlog\n",
		},
	}
	frozen := time.Date(2026, 5, 29, 12, 34, 56, 123456789, time.UTC)
	s := newSamplerWithClock(t, runner, dir, func() time.Time { return frozen })

	s.tick(context.Background())

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 5 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected 5 files, got %d: %v", len(entries), names)
	}

	expect := map[string]string{
		"show-hardware-capacity":  `{"capacity":1}`,
		"show-gre-tunnel-static":  `{"tunnels":[]}`,
		"show-processes-top-once": `{"processes":[]}`,
		"show-logging-errors":     "errlog\n",
		"show-logging-critical":   "critlog\n",
	}
	for prefix, want := range expect {
		matches, _ := filepath.Glob(filepath.Join(dir, prefix+"-*"))
		if len(matches) != 1 {
			t.Errorf("prefix %q: expected 1 match, got %d", prefix, len(matches))
			continue
		}
		got, err := os.ReadFile(matches[0])
		if err != nil {
			t.Errorf("read %s: %v", matches[0], err)
			continue
		}
		if string(got) != want {
			t.Errorf("file %s: body = %q, want %q", matches[0], string(got), want)
		}
	}
}

// TestSingleCommandFailureContinues confirms one failing command does
// not abort the tick: the other four files are still written.
func TestSingleCommandFailureContinues(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{
		errs: map[string]error{
			"show gre tunnel static": errors.New("boom"),
		},
	}
	frozen := time.Date(2026, 5, 29, 12, 34, 56, 0, time.UTC)
	s := newSamplerWithClock(t, runner, dir, func() time.Time { return frozen })

	s.tick(context.Background())

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 files after one failure, got %d", len(entries))
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "show-gre-tunnel-static") {
			t.Errorf("failing command should not have produced a file, got %s", e.Name())
		}
	}
}

// TestTwoTicksProduceDistinctFiles verifies the nanosecond-precision
// filename suffix prevents collisions between consecutive ticks.
func TestTwoTicksProduceDistinctFiles(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{}
	base := time.Date(2026, 5, 29, 12, 34, 56, 0, time.UTC)
	var n atomic.Int64
	s := newSamplerWithClock(t, runner, dir, func() time.Time {
		i := n.Add(1)
		return base.Add(time.Duration(i) * time.Nanosecond)
	})

	s.tick(context.Background())
	s.tick(context.Background())

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 10 {
		t.Fatalf("expected 10 files across two ticks, got %d", len(entries))
	}
	seen := map[string]struct{}{}
	for _, e := range entries {
		if _, dup := seen[e.Name()]; dup {
			t.Fatalf("duplicate filename %s", e.Name())
		}
		seen[e.Name()] = struct{}{}
	}
}

// TestRunCancelsCleanly confirms Run returns nil promptly after context
// cancellation.
func TestRunCancelsCleanly(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{}
	s := NewSampler(runner, dir, time.Hour, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// Give the initial tick a moment to run, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error after cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of cancel")
	}
}

// TestFileTimestampNoColon ensures the produced suffix is filesystem-safe.
func TestFileTimestampNoColon(t *testing.T) {
	got := fileTimestamp(time.Date(2026, 5, 29, 12, 34, 56, 123, time.UTC))
	if strings.Contains(got, ":") {
		t.Errorf("timestamp %q must not contain ':'", got)
	}
	if !strings.HasSuffix(got, "Z") {
		t.Errorf("timestamp %q must be UTC (end with Z)", got)
	}
	if !strings.HasPrefix(got, "2026-05-29T12-34-56") {
		t.Errorf("timestamp %q has unexpected prefix", got)
	}
}

// Sampler must satisfy collector.Collector via Run(ctx) error.
type runnable interface {
	Run(ctx context.Context) error
}

var _ runnable = (*Sampler)(nil)
