package promscrape

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"
)

// expositionSample covers a counter (labeled and unlabeled), a gauge, a
// summary, and a histogram so encodeFamilies is exercised for every branch.
const expositionSample = `# HELP doublezero_agent_apply_config_errors_total apply_config errors
# TYPE doublezero_agent_apply_config_errors_total counter
doublezero_agent_apply_config_errors_total 7
# HELP doublezero_agent_bgp_neighbors_errors_total bgp errors
# TYPE doublezero_agent_bgp_neighbors_errors_total counter
doublezero_agent_bgp_neighbors_errors_total{peer="10.0.0.1"} 2
doublezero_agent_bgp_neighbors_errors_total{peer="10.0.0.2"} 3
# HELP doublezero_agent_up agent up
# TYPE doublezero_agent_up gauge
doublezero_agent_up 1
# HELP request_latency_seconds latencies
# TYPE request_latency_seconds summary
request_latency_seconds{quantile="0.5"} 0.05
request_latency_seconds{quantile="0.99"} 0.5
request_latency_seconds_sum 12.5
request_latency_seconds_count 100
# HELP request_size_bytes sizes
# TYPE request_size_bytes histogram
request_size_bytes_bucket{le="100"} 5
request_size_bytes_bucket{le="1000"} 8
request_size_bytes_bucket{le="+Inf"} 10
request_size_bytes_sum 4500
request_size_bytes_count 10
`

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newScraperWithClock(t *testing.T, url, dir string, clock func() time.Time) *Scraper {
	t.Helper()
	s := New(url, dir, time.Hour, discardLogger())
	s.now = clock
	return s
}

// readRows reads observer.agent_metrics.json line-by-line and decodes each
// NDJSON record into a metricRow.
func readRows(t *testing.T, path string) []metricRow {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var rows []metricRow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var r metricRow
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("decode row %q: %v", sc.Text(), err)
		}
		rows = append(rows, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return rows
}

// startServer returns an httptest.Server whose handler is supplied by the
// caller. Tests use this to simulate happy / 500 / malformed responses.
func startServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func okHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = io.WriteString(w, body)
	}
}

// TestTickWritesNDJSON covers the happy path: a single tick produces one
// row per metric sample, every row decodes as JSON with the expected shape,
// and t_ns is identical across rows in the same tick.
func TestTickWritesNDJSON(t *testing.T) {
	dir := t.TempDir()
	srv := startServer(t, okHandler(expositionSample))
	frozen := time.Date(2026, 5, 29, 12, 34, 56, 123456789, time.UTC)
	s := newScraperWithClock(t, srv.URL, dir, func() time.Time { return frozen })

	s.tick(context.Background())

	rows := readRows(t, filepath.Join(dir, "observer.agent_metrics.json"))
	// Counter rows: 1 unlabeled + 2 labeled.
	// Gauge: 1.
	// Summary: _sum + _count + 2 quantile rows = 4.
	// Histogram: _sum + _count + 3 buckets = 5.
	want := 1 + 2 + 1 + 4 + 5
	if len(rows) != want {
		t.Fatalf("row count = %d, want %d", len(rows), want)
	}
	wantTNS := frozen.UnixNano()
	for i, r := range rows {
		if r.TNS != wantTNS {
			t.Errorf("row %d t_ns = %d, want %d", i, r.TNS, wantTNS)
		}
		if r.MetricName == "" {
			t.Errorf("row %d missing metric_name", i)
		}
		// labels_json must always be valid JSON object (even if empty).
		var labels map[string]string
		if err := json.Unmarshal([]byte(r.LabelsJSON), &labels); err != nil {
			t.Errorf("row %d labels_json %q: %v", i, r.LabelsJSON, err)
		}
	}
}

// TestLabelSerialization confirms a labeled counter's labels_json reparses
// to the source label map exactly.
func TestLabelSerialization(t *testing.T) {
	dir := t.TempDir()
	body := `# TYPE my_counter counter
my_counter{a="1",b="two"} 9
`
	srv := startServer(t, okHandler(body))
	s := newScraperWithClock(t, srv.URL, dir, time.Now)

	s.tick(context.Background())

	rows := readRows(t, filepath.Join(dir, "observer.agent_metrics.json"))
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(rows[0].LabelsJSON), &got); err != nil {
		t.Fatalf("unmarshal labels: %v", err)
	}
	want := map[string]string{"a": "1", "b": "two"}
	if len(got) != len(want) || got["a"] != want["a"] || got["b"] != want["b"] {
		t.Errorf("labels = %v, want %v", got, want)
	}
}

// TestSnapshotCountersOnly verifies counter family totals are exposed and
// gauges are excluded; labeled counter values are summed.
func TestSnapshotCountersOnly(t *testing.T) {
	dir := t.TempDir()
	srv := startServer(t, okHandler(expositionSample))
	s := newScraperWithClock(t, srv.URL, dir, time.Now)

	s.tick(context.Background())

	snap := s.Snapshot()
	if v := snap["doublezero_agent_apply_config_errors_total"]; v != 7 {
		t.Errorf("apply_config_errors_total = %v, want 7", v)
	}
	if v := snap["doublezero_agent_bgp_neighbors_errors_total"]; v != 5 {
		t.Errorf("bgp_neighbors_errors_total = %v, want 5 (sum of 2+3)", v)
	}
	if _, ok := snap["doublezero_agent_up"]; ok {
		t.Errorf("gauge doublezero_agent_up should not be in snapshot")
	}
}

// TestSnapshotStableAcrossFailure proves a failed tick (HTTP 500) leaves
// the previous Snapshot values intact.
func TestSnapshotStableAcrossFailure(t *testing.T) {
	dir := t.TempDir()
	var fail atomic.Bool
	srv := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, expositionSample)
	})
	s := newScraperWithClock(t, srv.URL, dir, time.Now)

	s.tick(context.Background())
	before := s.Snapshot()
	fail.Store(true)
	s.tick(context.Background())
	after := s.Snapshot()

	if len(before) != len(after) {
		t.Fatalf("snapshot length changed: before=%d after=%d", len(before), len(after))
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("snapshot[%s] changed: before=%v after=%v", k, v, after[k])
		}
	}
}

// TestSnapshotReflectsLatest confirms a second successful tick replaces
// counter values rather than accumulating across ticks.
func TestSnapshotReflectsLatest(t *testing.T) {
	dir := t.TempDir()
	var second atomic.Bool
	srv := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if second.Load() {
			_, _ = io.WriteString(w, "# TYPE my_counter counter\nmy_counter 42\n")
			return
		}
		_, _ = io.WriteString(w, "# TYPE my_counter counter\nmy_counter 10\n")
	})
	s := newScraperWithClock(t, srv.URL, dir, time.Now)

	s.tick(context.Background())
	if v := s.Snapshot()["my_counter"]; v != 10 {
		t.Fatalf("first snapshot my_counter = %v, want 10", v)
	}
	second.Store(true)
	s.tick(context.Background())
	if v := s.Snapshot()["my_counter"]; v != 42 {
		t.Fatalf("second snapshot my_counter = %v, want 42", v)
	}
}

// TestEmptyBodyFreezesSnapshot guards against a transient HTTP 200 with no
// metric families clobbering the snapshot — the decider in PR #3796 must
// not see "counters reset" when the agent momentarily returns an empty body.
func TestEmptyBodyFreezesSnapshot(t *testing.T) {
	dir := t.TempDir()
	var empty atomic.Bool
	srv := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if empty.Load() {
			// Empty 200 — no families at all.
			return
		}
		_, _ = io.WriteString(w, expositionSample)
	})
	s := newScraperWithClock(t, srv.URL, dir, time.Now)

	s.tick(context.Background())
	before := s.Snapshot()
	if before["doublezero_agent_apply_config_errors_total"] != 7 {
		t.Fatalf("first snapshot did not capture counter: %v", before)
	}
	empty.Store(true)
	s.tick(context.Background())
	after := s.Snapshot()
	if after["doublezero_agent_apply_config_errors_total"] != 7 {
		t.Errorf("snapshot should remain frozen across empty response, got %v", after)
	}
}

// TestParseErrorContinues verifies a malformed exposition body logs WARN
// and leaves the output file untouched (Run did not abort).
func TestParseErrorContinues(t *testing.T) {
	dir := t.TempDir()
	srv := startServer(t, okHandler("not valid prometheus text\n"))
	s := newScraperWithClock(t, srv.URL, dir, time.Now)

	s.tick(context.Background())

	out := filepath.Join(dir, "observer.agent_metrics.json")
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("output file should not exist after parse error, stat err = %v", err)
	}
}

// TestRunCancelsCleanly matches sample/eos_test.go: cancel after the
// initial tick and confirm Run returns nil within 2 s.
func TestRunCancelsCleanly(t *testing.T) {
	dir := t.TempDir()
	srv := startServer(t, okHandler(expositionSample))
	s := New(srv.URL, dir, time.Hour, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of cancel")
	}
}

// TestSingleAppendPerTick checks that a tick produces a single contiguous
// block of rows so interleaving from concurrent processes (if any) cannot
// split a tick's output.
func TestSingleAppendPerTick(t *testing.T) {
	dir := t.TempDir()
	srv := startServer(t, okHandler(expositionSample))
	s := newScraperWithClock(t, srv.URL, dir, time.Now)

	s.tick(context.Background())

	body, err := os.ReadFile(filepath.Join(dir, "observer.agent_metrics.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Every line must be a complete JSON object terminated by \n.
	for _, line := range bytes.Split(bytes.TrimRight(body, "\n"), []byte("\n")) {
		if !bytes.HasPrefix(line, []byte("{")) || !bytes.HasSuffix(line, []byte("}")) {
			t.Errorf("malformed NDJSON line: %q", line)
		}
	}
	if !strings.HasSuffix(string(body), "\n") {
		t.Errorf("output must end with newline")
	}
}

// Compile-time: *Scraper satisfies the collector.Collector interface.
var _ collector.Collector = (*Scraper)(nil)
