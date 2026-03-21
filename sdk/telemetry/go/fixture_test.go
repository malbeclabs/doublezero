package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"testing"

	"github.com/gagliardetto/solana-go"
)

type fixtureMeta struct {
	Name        string       `json:"name"`
	AccountType int          `json:"account_type"`
	Fields      []fieldValue `json:"fields"`
}

type fieldValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  string `json:"typ"`
}

func fixturesDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "testdata", "fixtures")
}

func loadFixture(t *testing.T, name string) ([]byte, fixtureMeta) {
	t.Helper()
	dir := fixturesDir()

	binData, err := os.ReadFile(filepath.Join(dir, name+".bin"))
	if err != nil {
		t.Fatalf("reading %s.bin: %v", name, err)
	}

	jsonData, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		t.Fatalf("reading %s.json: %v", name, err)
	}

	var meta fixtureMeta
	if err := json.Unmarshal(jsonData, &meta); err != nil {
		t.Fatalf("parsing %s.json: %v", name, err)
	}

	return binData, meta
}

func TestFixtureDeviceLatencySamples(t *testing.T) {
	data, meta := loadFixture(t, "device_latency_samples")
	d, err := DeserializeDeviceLatencySamples(data)
	if err != nil {
		t.Fatalf("DeserializeDeviceLatencySamples: %v", err)
	}

	assertFields(t, meta.Fields, map[string]any{
		"AccountType":                  uint8(d.AccountType),
		"Epoch":                        d.Epoch,
		"OriginDeviceAgentPK":          solana.PublicKey(d.OriginDeviceAgentPK),
		"OriginDevicePK":               solana.PublicKey(d.OriginDevicePK),
		"TargetDevicePK":               solana.PublicKey(d.TargetDevicePK),
		"OriginDeviceLocationPK":       solana.PublicKey(d.OriginDeviceLocationPK),
		"TargetDeviceLocationPK":       solana.PublicKey(d.TargetDeviceLocationPK),
		"LinkPK":                       solana.PublicKey(d.LinkPK),
		"SamplingIntervalMicroseconds": d.SamplingIntervalMicroseconds,
		"StartTimestampMicroseconds":   d.StartTimestampMicroseconds,
		"NextSampleIndex":              d.NextSampleIndex,
		"SamplesCount":                 uint32(len(d.Samples)),
	})
}

func TestFixtureInternetLatencySamples(t *testing.T) {
	data, meta := loadFixture(t, "internet_latency_samples")
	d, err := DeserializeInternetLatencySamples(data)
	if err != nil {
		t.Fatalf("DeserializeInternetLatencySamples: %v", err)
	}

	assertFields(t, meta.Fields, map[string]any{
		"AccountType":                  uint8(d.AccountType),
		"Epoch":                        d.Epoch,
		"OracleAgentPK":                solana.PublicKey(d.OracleAgentPK),
		"OriginExchangePK":             solana.PublicKey(d.OriginExchangePK),
		"TargetExchangePK":             solana.PublicKey(d.TargetExchangePK),
		"SamplingIntervalMicroseconds": d.SamplingIntervalMicroseconds,
		"StartTimestampMicroseconds":   d.StartTimestampMicroseconds,
		"NextSampleIndex":              d.NextSampleIndex,
		"SamplesCount":                 uint32(len(d.Samples)),
	})
}

func assertFields(t *testing.T, expected []fieldValue, got map[string]any) {
	t.Helper()
	for _, f := range expected {
		val, ok := got[f.Name]
		if !ok {
			continue
		}
		switch f.Type {
		case "u8":
			want, _ := strconv.ParseUint(f.Value, 10, 8)
			assertEq(t, f.Name, uint8(want), val)
		case "u16":
			want, _ := strconv.ParseUint(f.Value, 10, 16)
			assertEq(t, f.Name, uint16(want), val)
		case "u32":
			want, _ := strconv.ParseUint(f.Value, 10, 32)
			assertEq(t, f.Name, uint32(want), val)
		case "u64":
			want, _ := strconv.ParseUint(f.Value, 10, 64)
			assertEq(t, f.Name, uint64(want), val)
		case "pubkey":
			want := solana.MustPublicKeyFromBase58(f.Value)
			assertEq(t, f.Name, want, val)
		}
	}
}

func TestFixtureTimestampIndex(t *testing.T) {
	data, meta := loadFixture(t, "timestamp_index")
	d, err := DeserializeTimestampIndex(data)
	if err != nil {
		t.Fatalf("DeserializeTimestampIndex: %v", err)
	}

	got := map[string]any{
		"AccountType":      uint8(d.AccountType),
		"SamplesAccountPK": solana.PublicKey(d.SamplesAccountPK),
		"NextEntryIndex":   d.NextEntryIndex,
		"EntriesCount":     uint32(len(d.Entries)),
	}
	if len(d.Entries) > 0 {
		got["Entry0SampleIndex"] = d.Entries[0].SampleIndex
		got["Entry0Timestamp"] = d.Entries[0].TimestampMicroseconds
	}
	if len(d.Entries) > 1 {
		got["Entry1SampleIndex"] = d.Entries[1].SampleIndex
		got["Entry1Timestamp"] = d.Entries[1].TimestampMicroseconds
	}
	if len(d.Entries) > 2 {
		got["Entry2SampleIndex"] = d.Entries[2].SampleIndex
		got["Entry2Timestamp"] = d.Entries[2].TimestampMicroseconds
	}

	assertFields(t, meta.Fields, got)
}

func TestReconstructTimestamp(t *testing.T) {
	interval := uint64(5_000_000) // 5s in µs
	entries := []TimestampIndexEntry{
		{SampleIndex: 0, TimestampMicroseconds: 1_700_000_000_000_000},
		{SampleIndex: 12, TimestampMicroseconds: 1_700_000_000_120_000},
		{SampleIndex: 24, TimestampMicroseconds: 1_700_000_000_240_000},
	}

	// Sample 0: first entry, offset 0
	ts := ReconstructTimestamp(entries, 0, 0, interval)
	assertEq(t, "sample0", uint64(1_700_000_000_000_000), ts)

	// Sample 5: first entry, offset 5
	ts = ReconstructTimestamp(entries, 5, 0, interval)
	assertEq(t, "sample5", uint64(1_700_000_000_000_000+5*5_000_000), ts)

	// Sample 12: second entry, offset 0
	ts = ReconstructTimestamp(entries, 12, 0, interval)
	assertEq(t, "sample12", uint64(1_700_000_000_120_000), ts)

	// Sample 15: second entry, offset 3
	ts = ReconstructTimestamp(entries, 15, 0, interval)
	assertEq(t, "sample15", uint64(1_700_000_000_120_000+3*5_000_000), ts)

	// Sample 30: third entry, offset 6
	ts = ReconstructTimestamp(entries, 30, 0, interval)
	assertEq(t, "sample30", uint64(1_700_000_000_240_000+6*5_000_000), ts)
}

func TestReconstructTimestampFallback(t *testing.T) {
	// No entries — falls back to implicit model.
	ts := ReconstructTimestamp(nil, 10, 1_700_000_000_000_000, 5_000_000)
	assertEq(t, "fallback", uint64(1_700_000_000_000_000+10*5_000_000), ts)
}

func TestReconstructTimestamps(t *testing.T) {
	entries := []TimestampIndexEntry{
		{SampleIndex: 0, TimestampMicroseconds: 1000},
		{SampleIndex: 3, TimestampMicroseconds: 5000},
	}
	ts := ReconstructTimestamps(5, entries, 0, 100)
	expected := []uint64{1000, 1100, 1200, 5000, 5100}
	for i, want := range expected {
		assertEq(t, "ts_"+strconv.Itoa(i), want, ts[i])
	}
}

func TestReconstructTimestamp_LateStart(t *testing.T) {
	// Timestamp index created mid-epoch: first entry starts at sample 120.
	// Samples 0..119 should fall back to the implicit model.
	startTS := uint64(1_700_000_000_000_000)
	interval := uint64(5_000_000) // 5s in µs
	entries := []TimestampIndexEntry{
		{SampleIndex: 120, TimestampMicroseconds: 1_700_000_000_800_000},
		{SampleIndex: 240, TimestampMicroseconds: 1_700_000_001_600_000},
	}

	// Sample 0: before first entry, should use implicit model.
	ts := ReconstructTimestamp(entries, 0, startTS, interval)
	assertEq(t, "sample0", startTS, ts)

	// Sample 50: still before first entry.
	ts = ReconstructTimestamp(entries, 50, startTS, interval)
	assertEq(t, "sample50", startTS+50*interval, ts)

	// Sample 119: last sample before first entry.
	ts = ReconstructTimestamp(entries, 119, startTS, interval)
	assertEq(t, "sample119", startTS+119*interval, ts)

	// Sample 120: exactly at first entry.
	ts = ReconstructTimestamp(entries, 120, startTS, interval)
	assertEq(t, "sample120", uint64(1_700_000_000_800_000), ts)

	// Sample 125: within first entry's range.
	ts = ReconstructTimestamp(entries, 125, startTS, interval)
	assertEq(t, "sample125", uint64(1_700_000_000_800_000+5*interval), ts)

	// Sample 240: at second entry.
	ts = ReconstructTimestamp(entries, 240, startTS, interval)
	assertEq(t, "sample240", uint64(1_700_000_001_600_000), ts)
}

func TestReconstructTimestamps_LateStart(t *testing.T) {
	// First entry starts at sample 3, so samples 0..2 use implicit model.
	startTS := uint64(1000)
	interval := uint64(100)
	entries := []TimestampIndexEntry{
		{SampleIndex: 3, TimestampMicroseconds: 5000},
	}
	ts := ReconstructTimestamps(5, entries, startTS, interval)
	expected := []uint64{1000, 1100, 1200, 5000, 5100}
	for i, want := range expected {
		assertEq(t, "ts_"+strconv.Itoa(i), want, ts[i])
	}
}

func assertEq(t *testing.T, name string, want, got any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("%s: want %v, got %v", name, want, got)
	}
}
