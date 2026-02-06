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

func assertEq(t *testing.T, name string, want, got any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("%s: want %v, got %v", name, want, got)
	}
}
