package parser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadProcessTopSamples_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Two snapshots, second one taken 10 s after the first. CPU sums
	// match the inputs (the "idle" field is sentinel-only and must not
	// contribute to the total).
	a := `{
		"cpuInfo": {"%Cpu(s)": {"idle": 70.0, "user": 20.0, "system": 5.0, "ioWait": 5.0}},
		"memInfo": {"physicalMem": {"memFree": 200000, "memUsed": 100000, "memTotal": 300000}},
		"timeInfo": {"currentTime": 1700000000.0}
	}`
	b := `{
		"cpuInfo": {"%Cpu(s)": {"idle": 10.0, "user": 80.0, "system": 5.0, "ioWait": 5.0}},
		"memInfo": {"physicalMem": {"memFree": 150000, "memUsed": 150000, "memTotal": 300000}},
		"timeInfo": {"currentTime": 1700000010.0}
	}`
	// Write second sample first to verify the loader sorts by sample
	// time, not filename / readdir order.
	if err := os.WriteFile(filepath.Join(dir, "show-processes-top-once-2023-11-14T22-13-30.000000000Z.json"), []byte(b), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "show-processes-top-once-2023-11-14T22-13-20.000000000Z.json"), []byte(a), 0o600); err != nil {
		t.Fatal(err)
	}

	got, skipped, err := LoadProcessTopSamples(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 samples, got %d", len(got))
	}
	if skipped != 0 {
		t.Errorf("want 0 skipped, got %d", skipped)
	}
	if !got[0].At.Before(got[1].At) {
		t.Fatalf("samples not sorted by time: %v then %v", got[0].At, got[1].At)
	}
	if got[0].CPUPercent != 30.0 {
		t.Errorf("first CPU: want 30.0, got %v", got[0].CPUPercent)
	}
	if got[1].CPUPercent != 90.0 {
		t.Errorf("second CPU: want 90.0, got %v", got[1].CPUPercent)
	}
	if got[0].MemFreeKB != 200000 || got[0].MemTotalKB != 300000 {
		t.Errorf("first mem fields wrong: %+v", got[0])
	}
}

func TestLoadProcessTopSamples_CountsSkipped(t *testing.T) {
	dir := t.TempDir()
	// One valid sample, one garbage sample. Loader should return the
	// valid one and report skipped=1 so the writer can warn.
	good := `{
		"cpuInfo": {"%Cpu(s)": {"idle": 50.0, "user": 50.0}},
		"memInfo": {"physicalMem": {"memFree": 1, "memUsed": 1, "memTotal": 1}},
		"timeInfo": {"currentTime": 1700000000.0}
	}`
	if err := os.WriteFile(filepath.Join(dir, "show-processes-top-once-2023-11-14T22-13-20.000000000Z.json"), []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "show-processes-top-once-2023-11-14T22-13-30.000000000Z.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, skipped, err := LoadProcessTopSamples(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("want 1 sample loaded, got %d", len(got))
	}
	if skipped != 1 {
		t.Errorf("want 1 skipped, got %d", skipped)
	}
}

func TestLoadProcessTopSamples_FallsBackToFilenameTimestamp(t *testing.T) {
	dir := t.TempDir()
	// Omit timeInfo.currentTime → loader should parse the filename
	// timestamp instead.
	body := `{
		"cpuInfo": {"%Cpu(s)": {"idle": 99.0, "user": 1.0}},
		"memInfo": {"physicalMem": {"memFree": 1, "memUsed": 1, "memTotal": 1}}
	}`
	if err := os.WriteFile(filepath.Join(dir, "show-processes-top-once-2023-11-14T22-13-20.123456789Z.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadProcessTopSamples(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 sample, got %d", len(got))
	}
	want := time.Date(2023, 11, 14, 22, 13, 20, 123456789, time.UTC)
	if !got[0].At.Equal(want) {
		t.Errorf("filename timestamp: want %v, got %v", want, got[0].At)
	}
}

func TestLoadProcessTopSamples_MissingDir(t *testing.T) {
	// Glob against a non-existent directory: filepath.Glob returns no
	// matches with no error, so the loader yields an empty slice — the
	// same shape as a run with the observer disabled.
	got, skipped, err := LoadProcessTopSamples(filepath.Join(t.TempDir(), "no-such"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || skipped != 0 {
		t.Errorf("want 0/0, got %d/%d", len(got), skipped)
	}
}

func TestLoadAgentMetrics_FiltersByMetricName(t *testing.T) {
	dir := t.TempDir()
	// Three valid rows (two matching, one not) plus a malformed line.
	// We expect: two matching rows returned in t_ns order, the
	// unrelated row dropped silently, and skipped == 1 for the
	// malformed line.
	body := "" +
		`{"t_ns":2000,"metric_name":"process_resident_memory_bytes","value":200,"labels_json":"{}"}` + "\n" +
		`{"t_ns":1000,"metric_name":"process_resident_memory_bytes","value":100,"labels_json":"{}"}` + "\n" +
		`{"t_ns":1500,"metric_name":"some_other_metric","value":999,"labels_json":"{}"}` + "\n" +
		`not even json` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "observer.agent_metrics.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, skipped, err := LoadAgentMetrics(dir, "process_resident_memory_bytes")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	if skipped != 1 {
		t.Errorf("want 1 skipped, got %d", skipped)
	}
	if got[0].TNS != 1000 || got[1].TNS != 2000 {
		t.Errorf("rows not sorted by t_ns: %+v", got)
	}
	if got[0].Value != 100 || got[1].Value != 200 {
		t.Errorf("values wrong: %+v", got)
	}
}

func TestLoadAgentMetrics_MissingFile(t *testing.T) {
	got, skipped, err := LoadAgentMetrics(t.TempDir(), "anything")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || skipped != 0 {
		t.Errorf("want 0/0, got %d/%d", len(got), skipped)
	}
}
