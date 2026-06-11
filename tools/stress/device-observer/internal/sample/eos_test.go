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
			"show hardware capacity":      json.RawMessage(`{"capacity":1}`),
			"show interfaces description": json.RawMessage(`{"interfaceDescriptions":{}}`),
			"show processes top once":     json.RawMessage(`{"processes":[]}`),
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
		"show-hardware-capacity":      `{"capacity":1}`,
		"show-interfaces-description": `{"interfaceDescriptions":{}}`,
		"show-processes-top-once":     `{"processes":[]}`,
		"show-logging-errors":         "errlog\n",
		"show-logging-critical":       "critlog\n",
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
			"show interfaces description": errors.New("boom"),
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
		if strings.HasPrefix(e.Name(), "show-interfaces-description") {
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

// TestParseCPUPercent exercises the parser against the EOS eAPI JSON
// shape returned by `show processes top once | json`. The primary
// fixture is a verbatim capture from a cEOS device.
func TestParseCPUPercent(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "ceos-show-processes-top-once.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Fixture has user=1, system=0.3, all others 0, idle=98.7.
	const want = 1.3
	got, ok := parseCPUPercent(raw)
	if !ok {
		t.Fatal("parseCPUPercent returned ok=false for cEOS fixture")
	}
	const eps = 0.001
	if got < want-eps || got > want+eps {
		t.Errorf("got = %v, want %v", got, want)
	}

	t.Run("idle only", func(t *testing.T) {
		body := []byte(`{"cpuInfo":{"%Cpu(s)":{"idle":100,"user":0,"system":0}}}`)
		got, ok := parseCPUPercent(body)
		if !ok {
			t.Fatal("ok=false")
		}
		if got != 0 {
			t.Errorf("got = %v, want 0", got)
		}
	})

	t.Run("missing idle field", func(t *testing.T) {
		// No `idle` key means the response is malformed or partial;
		// reject rather than report a misleading non-zero sum.
		body := []byte(`{"cpuInfo":{"%Cpu(s)":{"user":1,"system":2}}}`)
		if _, ok := parseCPUPercent(body); ok {
			t.Error("expected ok=false when idle field absent")
		}
	})

	t.Run("missing cpuInfo", func(t *testing.T) {
		body := []byte(`{"processes":{}}`)
		if _, ok := parseCPUPercent(body); ok {
			t.Error("expected ok=false when cpuInfo absent")
		}
	})
}

// TestLatestCPUUpdated verifies a tick that succeeds on `show processes
// top once` updates the sampler's CPU snapshot.
func TestLatestCPUUpdated(t *testing.T) {
	dir := t.TempDir()
	envelope := []byte(`{"cpuInfo":{"%Cpu(s)":{"idle":95,"user":3,"system":2,"nice":0}}}`)
	runner := &fakeRunner{
		jsonResp: map[string]json.RawMessage{
			"show processes top once": envelope,
		},
	}
	s := newSamplerWithClock(t, runner, dir, func() time.Time {
		return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	})
	if _, ok := s.LatestCPUPercent(); ok {
		t.Fatal("LatestCPUPercent should be invalid before any tick")
	}
	s.tick(context.Background())
	pct, ok := s.LatestCPUPercent()
	if !ok {
		t.Fatal("LatestCPUPercent should be valid after a successful tick")
	}
	if pct < 4.9 || pct > 5.1 {
		t.Errorf("LatestCPUPercent = %v, want ~5", pct)
	}
}

// Sampler must satisfy collector.Collector via Run(ctx) error.
type runnable interface {
	Run(ctx context.Context) error
}

var _ runnable = (*Sampler)(nil)

// TestParseTunnelCount covers the shapes the parser must handle for
// `show interfaces description | json`:
//
//   - a populated map containing user tunnels (description begins
//     "USER-UCAST-") mixed with non-user interfaces (Loopback,
//     Ethernet, Management, inter-router fabric tunnels);
//   - a map containing no user tunnels (the device has only routing
//     fabric — valid state, not unknown);
//   - an empty map;
//   - a missing or malformed response (must signal ok=false so the
//     decider suppresses the device_tunnel_gap trigger).
//
// Coverage explicitly includes the low-id case (Tunnel1, Tunnel2)
// since the controller's gm/tunnel-id-start-1 fix allocates user
// tunnels from index 1 — the previous numeric-range filter rejected
// these but the description-prefix filter must accept them.
func TestParseTunnelCount(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		wantN  int
		wantOK bool
	}{
		{
			name: "populated map filters by USER-UCAST- description",
			body: `{"interfaceDescriptions":{
				"Loopback255":{"description":""},
				"Loopback256":{"description":""},
				"Ethernet1":{"description":""},
				"Management0":{"description":""},
				"Tunnel19":{"description":"to-chi-dn-dzd9"},
				"Tunnel1":{"description":"USER-UCAST-1"},
				"Tunnel2":{"description":"USER-UCAST-2"},
				"Tunnel500":{"description":"USER-UCAST-500"}
			}}`,
			wantN:  3,
			wantOK: true,
		},
		{
			name: "only routing-fabric tunnels present (no USER-UCAST- description)",
			body: `{"interfaceDescriptions":{
				"Tunnel19":{"description":"to-chi-dn-dzd9"},
				"Tunnel59":{"description":"to-chi-dn-dzd1"},
				"Tunnel91":{"description":"to-chi-dn-dzd5"}
			}}`,
			wantN:  0,
			wantOK: true,
		},
		{
			name:   "empty interfaceDescriptions map",
			body:   `{"interfaceDescriptions":{}}`,
			wantN:  0,
			wantOK: true,
		},
		{
			name:   "missing interfaceDescriptions key",
			body:   `{}`,
			wantN:  0,
			wantOK: false,
		},
		{
			name:   "malformed JSON",
			body:   `{nope`,
			wantN:  0,
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, ok := parseTunnelCount(json.RawMessage(c.body))
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if n != c.wantN {
				t.Fatalf("n = %d, want %d", n, c.wantN)
			}
		})
	}
}
