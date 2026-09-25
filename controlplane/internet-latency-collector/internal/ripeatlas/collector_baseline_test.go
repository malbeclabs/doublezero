package ripeatlas

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/stretchr/testify/require"
)

// recordingExporter implements exporter.Exporter and keeps every record written to it.
type recordingExporter struct {
	mu      sync.Mutex
	records []exporter.Record
}

func (e *recordingExporter) WriteRecords(ctx context.Context, records []exporter.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.records = append(e.records, records...)
	return nil
}

func (e *recordingExporter) Close() error {
	return nil
}

func (e *recordingExporter) written() []exporter.Record {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.records
}

// baselineLocations returns five locations, one probe each, listed out of alphabetical
// order so the ordering assertions cannot pass by accident.
func baselineLocations() []LocationProbeMatch {
	return []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "nyc", Latitude: 40.7128, Longitude: -74.0060},
			NearbyProbes:  []Probe{{ID: 100, Address: "192.0.2.1", Latitude: 40.7128, Longitude: -74.0060}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "lon", Latitude: 51.5074, Longitude: -0.1278},
			NearbyProbes:  []Probe{{ID: 200, Address: "192.0.2.2", Latitude: 51.5074, Longitude: -0.1278}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "ams", Latitude: 52.3676, Longitude: 4.9041},
			NearbyProbes:  []Probe{{ID: 300, Address: "192.0.2.3", Latitude: 52.3676, Longitude: 4.9041}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "sin", Latitude: 1.3521, Longitude: 103.8198},
			NearbyProbes:  []Probe{{ID: 400, Address: "192.0.2.4", Latitude: 1.3521, Longitude: 103.8198}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "chi", Latitude: 41.8781, Longitude: -87.6298},
			NearbyProbes:  []Probe{{ID: 500, Address: "192.0.2.5", Latitude: 41.8781, Longitude: -87.6298}},
			ProbeCount:    1,
		},
	}
}

func TestInternetLatency_RIPEAtlas_Baseline_OneMeasurementPerUnorderedPair(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := &Collector{client: &MockClient{}, log: log}
	measurementState := NewMeasurementState(filepath.Join(t.TempDir(), TimestampFileName))

	specs := c.generateWantedMeasurements(baselineLocations(), 1, measurementState)

	// Five locations give one measurement per target except the alphabetically last one,
	// whose pairs are all carried by earlier targets.
	require.Len(t, specs, 4, "expected one measurement per target except the last")

	targets := make([]string, 0, len(specs))
	for _, spec := range specs {
		targets = append(targets, spec.TargetLocationCode)
	}
	require.Equal(t, []string{"ams", "chi", "lon", "nyc"}, targets, "targets must be alphabetical")

	seen := map[string]bool{}
	enlistments := 0
	for _, spec := range specs {
		for _, source := range spec.SourceSpecs {
			pair := spec.TargetLocationCode + "|" + source.LocationCode
			reverse := source.LocationCode + "|" + spec.TargetLocationCode

			require.False(t, seen[pair], "pair %s measured twice", pair)
			require.False(t, seen[reverse], "reverse of pair %s already measured", pair)
			require.Less(t, spec.TargetLocationCode, source.LocationCode,
				"target %s must sort before source %s", spec.TargetLocationCode, source.LocationCode)

			seen[pair] = true
			enlistments++
		}
	}

	// Five locations give 5 * 4 / 2 = 10 unordered pairs, one enlistment each.
	require.Equal(t, 10, enlistments, "expected one enlistment per unordered pair")
}

func TestInternetLatency_RIPEAtlas_Baseline_TargetIsAlphabeticallyFirst(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var mu sync.Mutex
	var created []MeasurementRequest

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return []Measurement{}, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			created = append(created, request)
			measurementID := 3000 + len(created)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{measurementID}}, nil
		},
	}

	c := &Collector{client: mockClient, log: log, env: "mainnet-beta"}

	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "nyc", Latitude: 40.7128, Longitude: -74.0060},
			NearbyProbes:  []Probe{{ID: 100, Address: "192.0.2.1", Latitude: 40.7128, Longitude: -74.0060}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "ams", Latitude: 52.3676, Longitude: 4.9041},
			NearbyProbes:  []Probe{{ID: 300, Address: "192.0.2.3", Latitude: 52.3676, Longitude: 4.9041}},
			ProbeCount:    1,
		},
	}

	err := c.configureMeasurements(t.Context(), locationMatches, false, 1, t.TempDir(), 10*time.Minute)
	require.NoError(t, err)

	mu.Lock()
	requests := created
	mu.Unlock()

	require.Len(t, requests, 1, "one pair gives one measurement")
	require.Len(t, requests[0].Definitions, 1)

	definition := requests[0].Definitions[0]
	require.Equal(t, "ping", definition.Type)
	require.Equal(t, 4, definition.AF)
	require.Equal(t, 600, definition.Interval)
	require.Equal(t, "192.0.2.3", definition.Target, "ams sorts first, so its probe is the ping target")
	require.Equal(t, "DoubleZero [mainnet-beta] to ams probe 300", definition.Description)
	require.Equal(t, []string{"mainnet-beta", "doublezero"}, definition.Tags)

	require.Len(t, requests[0].Probes, 1, "the other location supplies the source probe")
	require.Equal(t, 100, requests[0].Probes[0].Value)
	require.Equal(t, "probes", requests[0].Probes[0].Type)
}

func TestInternetLatency_RIPEAtlas_Baseline_ExportSwapsSourceAndTarget(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return []Measurement{
				{
					ID:          1,
					Description: "DoubleZero [mainnet-beta] to ams probe 300",
					Status: struct {
						Name string `json:"name"`
						ID   int    `json:"id"`
					}{Name: "Ongoing"},
				},
			}, nil
		},
		GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
			return []any{
				map[string]any{
					"prb_id":    float64(100),
					"timestamp": float64(1609459260),
					"result":    []any{map[string]any{"rtt": float64(26.0)}},
				},
				map[string]any{
					"prb_id":    float64(200),
					"timestamp": float64(1609459260),
					"result":    []any{map[string]any{"rtt": float64(80.5)}},
				},
			}, nil
		},
	}

	e := &recordingExporter{}
	stateDir := t.TempDir()

	c := &Collector{client: mockClient, log: log, exporter: e, env: "mainnet-beta"}

	measurementState := NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	measurementState.SetMetadata(1, MeasurementMeta{
		TargetLocation: "ams",
		TargetProbeID:  300,
		Sources: []SourceProbeMeta{
			{LocationCode: "nyc", ProbeID: 100},
			{LocationCode: "lon", ProbeID: 200},
		},
		CreatedAt: time.Now().Unix(),
	})
	require.NoError(t, measurementState.Save())

	require.NoError(t, c.ExportMeasurementResults(t.Context(), stateDir))

	records := e.written()
	require.Len(t, records, 2)

	for _, record := range records {
		require.Equal(t, exporter.DataProviderNameRIPEAtlas, record.DataProvider)
		require.Equal(t, "ams", record.SourceExchangeCode,
			"the pinged location is stored as the source")
		require.Contains(t, []string{"nyc", "lon"}, record.TargetExchangeCode,
			"the location that did the pinging is stored as the target")
		require.Less(t, record.SourceExchangeCode, record.TargetExchangeCode,
			"the stored source must sort before the stored target")
		require.Equal(t, time.Unix(1609459260, 0).UTC(), record.Timestamp)
	}

	require.Equal(t, 26*time.Millisecond, records[0].RTT)
	require.Equal(t, 80500*time.Microsecond, records[1].RTT)
}
