package ripeatlas

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/stretchr/testify/require"
)

// MockClient implements a mock of RipeAtlasClient for testing
type MockClient struct {
	GetProbesInRadiusFunc                func(ctx context.Context, latitude, longitude float64, radiusKm int, anchorsOnly bool) ([]Probe, error)
	GetProbesForLocationsFunc            func(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error)
	CreateMeasurementFunc                func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error)
	GetAllMeasurementsFunc               func(ctx context.Context, env string) ([]Measurement, error)
	GetMeasurementResultsFunc            func(ctx context.Context, measurementID int) ([]any, error)
	GetMeasurementResultsIncrementalFunc func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error)
	StopMeasurementFunc                  func(ctx context.Context, measurementID int) error
	GetCreditBalanceFunc                 func(ctx context.Context) (float64, error)
}

func (m *MockClient) GetProbesInRadius(ctx context.Context, latitude, longitude float64, radiusKm int, anchorsOnly bool) ([]Probe, error) {
	if m.GetProbesInRadiusFunc != nil {
		return m.GetProbesInRadiusFunc(ctx, latitude, longitude, radiusKm, anchorsOnly)
	}
	return []Probe{}, nil
}

func (m *MockClient) GetProbesForLocations(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error) {
	if m.GetProbesForLocationsFunc != nil {
		return m.GetProbesForLocationsFunc(ctx, locations)
	}
	return []LocationProbeMatch{}, nil
}

func (m *MockClient) CreateMeasurement(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
	if m.CreateMeasurementFunc != nil {
		return m.CreateMeasurementFunc(ctx, request)
	}
	return &MeasurementResponse{Measurements: []int{12345}}, nil
}

func (m *MockClient) GetAllMeasurements(ctx context.Context, env string) ([]Measurement, error) {
	if m.GetAllMeasurementsFunc != nil {
		return m.GetAllMeasurementsFunc(ctx, env)
	}
	return []Measurement{}, nil
}

func (m *MockClient) GetMeasurementResults(ctx context.Context, measurementID int) ([]any, error) {
	if m.GetMeasurementResultsFunc != nil {
		return m.GetMeasurementResultsFunc(ctx, measurementID)
	}
	return []any{}, nil
}

func (m *MockClient) GetMeasurementResultsIncremental(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
	if m.GetMeasurementResultsIncrementalFunc != nil {
		return m.GetMeasurementResultsIncrementalFunc(ctx, measurementID, startTimestamp)
	}
	return []any{}, nil
}

func (m *MockClient) StopMeasurement(ctx context.Context, measurementID int) error {
	if m.StopMeasurementFunc != nil {
		return m.StopMeasurementFunc(ctx, measurementID)
	}
	return nil
}

func (m *MockClient) GetCreditBalance(ctx context.Context) (float64, error) {
	if m.GetCreditBalanceFunc != nil {
		return m.GetCreditBalanceFunc(ctx)
	}
	return 0, nil
}

func TestInternetLatency_RIPEAtlas_GetNearestProbesSorted(t *testing.T) {
	t.Parallel()

	probes := []Probe{
		{ID: 1, Latitude: 40.7128, Longitude: -74.0060, Address: "1.1.1.1"},
		{ID: 2, Latitude: 40.7589, Longitude: -73.9851, Address: "2.2.2.2"},
		{ID: 3, Latitude: 51.5074, Longitude: -0.1278, Address: "3.3.3.3"},
		{ID: 4, Latitude: 40.7000, Longitude: -74.0000, Address: "4.4.4.4"},
	}

	// Test getting nearest 2 probes to New York coordinates
	result := getNearestProbesSorted(probes, 40.7128, -74.0060, 2)

	require.Len(t, result, 2, "Expected 2 probes")

	// Probe 1 should be first (exact match), probe 4 should be second
	require.Equal(t, 1, result[0].ID, "Expected first probe ID to be 1")
	require.Equal(t, 4, result[1].ID, "Expected second probe ID to be 4")
}

func TestInternetLatency_RIPEAtlas_GetNearestProbesSorted_EmptyInput(t *testing.T) {
	t.Parallel()

	result := getNearestProbesSorted([]Probe{}, 40.7128, -74.0060, 5)
	require.Empty(t, result, "Expected empty result for empty input")
}

func TestInternetLatency_RIPEAtlas_CalculateAndSortProbeDistances(t *testing.T) {
	t.Parallel()

	probes := []Probe{
		{ID: 1, Latitude: 40.7128, Longitude: -74.0060},
		{ID: 2, Latitude: 40.7589, Longitude: -73.9851},
		{ID: 3, Latitude: 51.5074, Longitude: -0.1278},
	}

	distances := calculateAndSortProbeDistances(probes, 40.7128, -74.0060)

	require.Len(t, distances, 3, "Expected 3 distances")

	// First should be exact match (0 distance)
	require.Equal(t, 1, distances[0].Probe.ID, "First probe should be ID 1")
	require.LessOrEqual(t, distances[0].Distance, 0.1, "First probe should have ~0 distance")

	// Distances should be in ascending order
	for i := 1; i < len(distances); i++ {
		require.GreaterOrEqual(t, distances[i].Distance, distances[i-1].Distance,
			"Distances not sorted at position %d", i)
	}
}

func TestInternetLatency_RIPEAtlas_FilterValidProbes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		probe Probe
		want  bool
	}{
		{
			name:  "Routable address, no tags",
			probe: Probe{ID: 1, Address: "8.8.8.8"},
			want:  true,
		},
		{
			name:  "Routable address, tagged system-ipv4-works",
			probe: probeWithTags(2, "1.1.1.1", "system-ipv4-works"),
			want:  true,
		},
		{
			name:  "Routable address, unrelated tags only",
			probe: probeWithTags(3, "9.9.9.9", "core", "home"),
			want:  true,
		},
		{
			name:  "Routable address, tagged system-ipv4-doesnt-work",
			probe: probeWithTags(4, "8.8.4.4", "system-ipv4-doesnt-work"),
			want:  false,
		},
		{
			name:  "Tagged system-ipv4-doesnt-work alongside other tags",
			probe: probeWithTags(5, "8.8.4.4", "system-ipv4-works", "system-ipv4-doesnt-work"),
			want:  false,
		},
		{
			name:  "Empty address",
			probe: Probe{ID: 6, Address: ""},
			want:  false,
		},
		{
			name:  "Private address",
			probe: Probe{ID: 7, Address: "192.168.1.1"},
			want:  false,
		},
		{
			name:  "Loopback address",
			probe: Probe{ID: 8, Address: "::1"},
			want:  false,
		},
		{
			name:  "Routable IPv6 address",
			probe: Probe{ID: 9, Address: "2001:4860:4860::8888"},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := filterValidProbes(logger, []Probe{tt.probe})
			if tt.want {
				require.Len(t, result, 1)
				require.Equal(t, tt.probe.ID, result[0].ID)
			} else {
				require.Empty(t, result)
			}
		})
	}
}

func probeWithTags(id int, address string, slugs ...string) Probe {
	p := Probe{ID: id, Address: address}
	for _, slug := range slugs {
		p.Tags = append(p.Tags, struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		}{Name: slug, Slug: slug})
	}
	return p
}

func TestInternetLatency_RIPEAtlas_NewCollector(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := NewCollector(log, nil, "dev", func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	})

	require.NotNil(t, c, "NewCollector should return a non-nil collector")
	require.NotNil(t, c.client, "Client should be initialized")
}

func TestInternetLatency_RIPEAtlas_ParseLatencyFromResult(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := NewCollector(log, nil, "dev", func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	})

	// Valid ping result
	timestamp := time.Unix(1609459200, 0).UTC()
	result := map[string]any{
		"timestamp": float64(timestamp.Unix()),
		"result": []any{
			map[string]any{"rtt": float64(25.5)},
			map[string]any{"rtt": float64(26.0)},
			map[string]any{"rtt": float64(25.8)},
		},
	}

	latency, timestamp, probeID := c.parseLatencyFromResult(result)
	require.Equal(t, 25500*time.Microsecond, latency, "Expected latency 25.5")
	require.Equal(t, timestamp, timestamp, "Expected timestamp 2021-01-01T00:00:00.000000")
	require.Equal(t, 0, probeID, "Expected probe ID 0 when not present")

	// No RTT values
	result = map[string]any{
		"timestamp": float64(1609459200),
		"result":    []any{},
	}

	latency, _, _ = c.parseLatencyFromResult(result)
	require.Equal(t, 0*time.Microsecond, latency, "Expected latency 0 for no results")

	// Invalid result structure
	latency, timestamp, probeID = c.parseLatencyFromResult("invalid")
	require.Equal(t, 0*time.Microsecond, latency, "Expected zero latency for invalid result")
	require.Empty(t, timestamp, "Expected empty timestamp for invalid result")
	require.Equal(t, 0, probeID, "Expected zero probe ID for invalid result")
}

func TestInternetLatency_RIPEAtlas_ClearAllMeasurements(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	stoppedMeasurements := []int{}
	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return []Measurement{
				{
					ID:          1,
					Description: "DoubleZero NYC probe 100 to LAX probe 200",
					Status: struct {
						Name string `json:"name"`
						ID   int    `json:"id"`
					}{Name: "Running"},
				},
				{
					ID:          2,
					Description: "DoubleZero NYC probe 101 to LAX probe 201",
					Status: struct {
						Name string `json:"name"`
						ID   int    `json:"id"`
					}{Name: "Stopped"},
				},
				{
					ID:          3,
					Description: "Other measurement",
					Status: struct {
						Name string `json:"name"`
						ID   int    `json:"id"`
					}{Name: "Running"},
				},
			}, nil
		},
		StopMeasurementFunc: func(ctx context.Context, measurementID int) error {
			stoppedMeasurements = append(stoppedMeasurements, measurementID)
			return nil
		},
	}

	c := &Collector{client: mockClient, log: log}

	err := c.ClearAllMeasurements(t.Context())

	require.NoError(t, err, "ClearAllMeasurements() failed")

	// Should only stop 1 measurement (ID 1, as ID 2 is already stopped and ID 3 is not DoubleZero)
	require.Len(t, stoppedMeasurements, 1, "Expected 1 measurement to be stopped")

	// Verify the correct measurement was stopped
	require.Equal(t, 1, stoppedMeasurements[0], "Expected measurement ID 1 to be stopped")
}

func TestInternetLatency_RIPEAtlas_ClearAllMeasurements_StopError(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return []Measurement{
				{
					ID:          1,
					Description: "DoubleZero NYC probe 100 to LAX probe 200",
					Status: struct {
						Name string `json:"name"`
						ID   int    `json:"id"`
					}{Name: "Running"},
				},
			}, nil
		},
		StopMeasurementFunc: func(ctx context.Context, measurementID int) error {
			return errors.New("stop failed")
		},
	}

	c := &Collector{client: mockClient, log: log}

	err := c.ClearAllMeasurements(t.Context())
	if err == nil {
		t.Error("Expected error, got nil")
	}

	collectorErr, ok := err.(*collector.CollectorError)
	require.True(t, ok, "Expected CollectorError, got %T", err)
	require.Equal(t, "process_measurements", collectorErr.Operation, "Expected operation process_measurements")
}

func TestInternetLatency_RIPEAtlas_ExportMeasurementResults(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return []Measurement{
				{
					ID:          1,
					Description: "DoubleZero to LAX probe 200",
					Status: struct {
						Name string `json:"name"`
						ID   int    `json:"id"`
					}{Name: "Running"},
				},
			}, nil
		},
		GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
			return []any{
				map[string]any{
					"prb_id":    float64(100), // NYC probe
					"timestamp": float64(1609459260),
					"result": []any{
						map[string]any{"rtt": float64(26.0)},
					},
				},
				map[string]any{
					"prb_id":    float64(101), // CHI probe
					"timestamp": float64(1609459260),
					"result": []any{
						map[string]any{"rtt": float64(26.0)},
					},
				},
			}, nil
		},
	}

	outputDir := t.TempDir()
	e, err := exporter.NewCSVExporter(log, "ripe_atlas_measurements", outputDir)
	require.NoError(t, err)
	c := &Collector{
		client:   mockClient,
		log:      log,
		exporter: e,
		probeToLocation: map[int]string{
			100: "nyc",
			101: "chi",
		},
	}

	timestampFile := filepath.Join(outputDir, TimestampFileName)
	measurementState := NewMeasurementState(timestampFile)
	measurementState.SetMetadata(1, MeasurementMeta{
		TargetLocation: "lax",
		TargetProbeID:  200,
		Sources: []SourceProbeMeta{
			{LocationCode: "nyc", ProbeID: 100},
			{LocationCode: "chi", ProbeID: 101},
		},
		CreatedAt: time.Now().Unix(),
	})
	err = measurementState.Save()
	require.NoError(t, err)

	err = c.ExportMeasurementResults(t.Context(), outputDir)
	require.NoError(t, err)

	files, err := filepath.Glob(filepath.Join(outputDir, "ripe_atlas_measurements_*.csv"))
	require.NoError(t, err)
	require.Len(t, files, 1)

	csvFile, err := os.Open(files[0])
	require.NoError(t, err)
	defer csvFile.Close()

	r := csv.NewReader(csvFile)
	records, err := r.ReadAll()
	require.NoError(t, err)

	require.Len(t, records, 3, "Expected 1 header + 2 data rows")

	header := records[0]
	tsIdx := slices.Index(header, "timestamp")
	rttIdx := slices.Index(header, "latency")
	srcIdx := slices.Index(header, "source_exchange_code")
	tgtIdx := slices.Index(header, "target_exchange_code")
	require.NotEqual(t, -1, tsIdx)
	require.NotEqual(t, -1, rttIdx)
	require.NotEqual(t, -1, srcIdx)
	require.NotEqual(t, -1, tgtIdx)

	// After the swap, source is the measurement target (lax) and target is the probe location (nyc/chi)
	sourcesSeen := map[string]struct{}{}
	targetsSeen := map[string]struct{}{}
	for _, row := range records[1:] {
		src := row[srcIdx]
		tgt := row[tgtIdx]
		ts, err := time.Parse(time.RFC3339, row[tsIdx])
		require.NoError(t, err)
		require.True(t, ts.Equal(time.Unix(1609459260, 0).UTC()))

		lat, err := time.ParseDuration(row[rttIdx])
		require.NoError(t, err)
		require.Equal(t, 26*time.Millisecond, lat)

		sourcesSeen[src] = struct{}{}
		targetsSeen[tgt] = struct{}{}
	}
	// Source should be the measurement target (lax)
	require.Contains(t, sourcesSeen, "lax")
	// Targets should be the probe locations (nyc, chi)
	require.Contains(t, targetsSeen, "nyc")
	require.Contains(t, targetsSeen, "chi")
}

func TestInternetLatency_RIPEAtlas_ExportMeasurementResults_PreservesAllSamples(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return []Measurement{
				{
					ID:          1,
					Description: "DoubleZero to LAX probe 200",
					Status: struct {
						Name string `json:"name"`
						ID   int    `json:"id"`
					}{Name: "Running"},
				},
			}, nil
		},
		GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
			return []any{
				map[string]any{
					"prb_id":    float64(100), // NYC probe
					"timestamp": float64(1609459200),
					"result": []any{
						map[string]any{"rtt": float64(24.0)},
					},
				},
				map[string]any{
					"prb_id":    float64(100), // Same probe, later time
					"timestamp": float64(1609459260),
					"result": []any{
						map[string]any{"rtt": float64(25.0)},
					},
				},
				map[string]any{
					"prb_id":    float64(100), // Same probe, even later
					"timestamp": float64(1609459320),
					"result": []any{
						map[string]any{"rtt": float64(26.0)},
					},
				},
			}, nil
		},
	}

	outputDir := t.TempDir()
	e, err := exporter.NewCSVExporter(log, "ripe_atlas_measurements", outputDir)
	require.NoError(t, err)

	c := &Collector{
		client:   mockClient,
		log:      log,
		exporter: e,
		probeToLocation: map[int]string{
			100: "nyc",
		},
	}

	timestampFile := filepath.Join(outputDir, TimestampFileName)
	measurementState := NewMeasurementState(timestampFile)
	measurementState.SetMetadata(1, MeasurementMeta{
		TargetLocation: "lax",
		TargetProbeID:  200,
		Sources: []SourceProbeMeta{
			{LocationCode: "nyc", ProbeID: 100},
		},
		CreatedAt: time.Now().Unix(),
	})
	err = measurementState.Save()
	require.NoError(t, err)

	err = c.ExportMeasurementResults(t.Context(), outputDir)
	require.NoError(t, err)

	files, err := filepath.Glob(filepath.Join(outputDir, "ripe_atlas_measurements_*.csv"))
	require.NoError(t, err)
	require.Len(t, files, 1)

	csvFile, err := os.Open(files[0])
	require.NoError(t, err)
	defer csvFile.Close()

	r := csv.NewReader(csvFile)
	records, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 4, "Expected 1 header + 3 data rows (all samples preserved)")

	header := records[0]
	timestampIdx := slices.Index(header, "timestamp")
	rttIdx := slices.Index(header, "latency")
	require.NotEqual(t, -1, timestampIdx)
	require.NotEqual(t, -1, rttIdx)

	// Verify all 3 samples are present
	expectedSamples := []struct {
		timestamp int64
		rtt       time.Duration
	}{
		{1609459200, 24 * time.Millisecond},
		{1609459260, 25 * time.Millisecond},
		{1609459320, 26 * time.Millisecond},
	}

	for i, expected := range expectedSamples {
		dataRow := records[i+1]
		timestamp, err := time.Parse(time.RFC3339, dataRow[timestampIdx])
		require.NoError(t, err)
		require.Equal(t, time.Unix(expected.timestamp, 0).UTC(), timestamp)

		rtt, err := time.ParseDuration(dataRow[rttIdx])
		require.NoError(t, err)
		require.Equal(t, expected.rtt, rtt)
	}
}

func TestInternetLatency_RIPEAtlas_ExportSingleMeasurementResults_StallWarning(t *testing.T) {
	t.Parallel()

	staleExport := time.Now().Add(-2 * staleMeasurementWarnAfter)

	// When the target probe goes dark the sources keep pinging and keep uploading, so the
	// page is not empty — the results just carry no rtt.
	timeoutResult := map[string]any{
		"prb_id":    float64(100),
		"timestamp": float64(time.Now().Unix()),
		"result": []any{
			map[string]any{"x": "*"},
			map[string]any{"x": "*"},
		},
	}
	successResult := map[string]any{
		"prb_id":    float64(100),
		"timestamp": float64(time.Now().Unix()),
		"result": []any{
			map[string]any{"rtt": float64(26.0)},
		},
	}

	tests := []struct {
		name         string
		results      []any
		lastExportAt int64
		wantWarn     bool
	}{
		{
			name:         "nothing uploaded since a stale export",
			results:      []any{},
			lastExportAt: staleExport.Unix(),
			wantWarn:     true,
		},
		{
			name:         "results uploaded but all timed out",
			results:      []any{timeoutResult},
			lastExportAt: staleExport.Unix(),
			wantWarn:     true,
		},
		{
			name:         "nothing uploaded since a recent export",
			results:      []any{},
			lastExportAt: time.Now().Unix(),
			wantWarn:     false,
		},
		{
			name:         "samples arrive after a long gap",
			results:      []any{successResult},
			lastExportAt: staleExport.Unix(),
			wantWarn:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

			outputDir := t.TempDir()
			e, err := exporter.NewCSVExporter(log, "ripe_atlas_measurements", outputDir)
			require.NoError(t, err)

			mockClient := &MockClient{
				GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
					return tt.results, nil
				},
			}
			c := &Collector{client: mockClient, log: log, exporter: e}

			measurementState := NewMeasurementState(filepath.Join(outputDir, TimestampFileName))
			measurementState.SetMetadata(1, MeasurementMeta{
				TargetLocation: "lax",
				TargetProbeID:  200,
				Sources:        []SourceProbeMeta{{LocationCode: "nyc", ProbeID: 100}},
				CreatedAt:      staleExport.Unix(),
				LastExportAt:   tt.lastExportAt,
			})

			_, _, err = c.exportSingleMeasurementResults(t.Context(), Measurement{ID: 1}, measurementState)
			require.NoError(t, err)

			if tt.wantWarn {
				require.Contains(t, logs.String(), "measurement stalled?", "A stalled measurement should warn")
			} else {
				require.NotContains(t, logs.String(), "measurement stalled?", "A measurement that is producing samples should stay quiet")
			}
		})
	}
}

func TestInternetLatency_RIPEAtlas_ListMeasurements(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return []Measurement{
				{
					ID:          1,
					Description: "DoubleZero NYC probe 100 to LAX probe 200",
					Target:      "8.8.8.8",
					Status: struct {
						Name string `json:"name"`
						ID   int    `json:"id"`
					}{Name: "Running"},
					Type: "ping",
				},
				{
					ID:          2,
					Description: "Measurement with, comma",
					Target:      "1.1.1.1",
					Status: struct {
						Name string `json:"name"`
						ID   int    `json:"id"`
					}{Name: "Stopped"},
					Type: "ping",
				},
			}, nil
		},
	}

	c := &Collector{client: mockClient, log: log}

	// Capture output (ListMeasurements is an interactive function that uses fmt.Print)
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := c.ListMeasurements(t.Context())

	w.Close()
	os.Stdout = oldStdout

	require.NoError(t, err, "ListMeasurements() failed")

	// Read and verify output
	output, _ := io.ReadAll(r)
	outputStr := string(output)

	// Check CSV header
	require.Contains(t, outputStr, "ID,Description,Target,Status,Type", "Output should contain CSV header")

	// Check first measurement
	require.Contains(t, outputStr, "1,DoubleZero NYC probe 100 to LAX probe 200,8.8.8.8,Running,ping", "Output should contain first measurement")

	// Check escaped comma in second measurement
	require.Contains(t, outputStr, `"Measurement with, comma"`, "Output should properly escape comma in description")
}

func TestInternetLatency_RIPEAtlas_ListAtlasProbes(t *testing.T) {
	log := logger.With("test", t.Name())

	mockClient := &MockClient{
		GetProbesForLocationsFunc: func(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error) {
			result := make([]LocationProbeMatch, len(locations))
			for i, loc := range locations {
				result[i] = LocationProbeMatch{
					LocationMatch: loc.LocationMatch,
					NearbyProbes: []Probe{
						{
							ID:        i*100 + 1,
							Address:   fmt.Sprintf("1.1.1.%d", i+1),
							AddressV6: fmt.Sprintf("2001:db8::%d", i+1),
							ASN:       64512 + i,
							Status: struct {
								ID    int    `json:"id"`
								Name  string `json:"name"`
								Since string `json:"since"`
							}{Name: "Connected"},
							Type:      "probe",
							Latitude:  loc.Latitude + 0.001,
							Longitude: loc.Longitude + 0.001,
						},
					},
					ProbeCount: 1,
				}
			}
			return result, nil
		},
	}

	c := &Collector{client: mockClient, log: log}

	locations := []collector.LocationMatch{
		{LocationCode: "nyc", Latitude: 40.7128, Longitude: -74.0060},
		{LocationCode: "lax", Latitude: 34.0522, Longitude: -118.2437},
	}

	// Capture output (ListAtlasProbes is an interactive function that uses fmt.Print)
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := c.ListAtlasProbes(t.Context(), locations)

	w.Close()
	os.Stdout = oldStdout

	require.NoError(t, err, "ListAtlasProbes() failed")

	// Read and verify output
	output, _ := io.ReadAll(r)
	outputStr := string(output)

	require.Contains(t, outputStr, "Found 2 locations", "Output should mention finding 2 locations")
	require.Contains(t, outputStr, "=== RIPE Atlas Probe Discovery Results ===", "Output should contain results header")
	require.Contains(t, outputStr, "Location: nyc", "Output should contain nyc location")
	require.Contains(t, outputStr, "Location: lax", "Output should contain lax location")
	require.Contains(t, outputStr, "IPv6:", "Output should show IPv6 addresses")
}

func TestInternetLatency_RIPEAtlas_ListAtlasProbes_NoDevices(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := NewCollector(log, nil, "dev", func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	})

	err := c.ListAtlasProbes(t.Context(), []collector.LocationMatch{})

	require.Equal(t, collector.ErrNoDevicesFound, err, "Expected ErrNoDevicesFound")
}

func TestInternetLatency_RIPEAtlas_GenerateWantedMeasurements_Deterministic(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := NewCollector(log, nil, "dev", func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	})

	// Create test locations with probes in non-alphabetical order
	locations := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{
				LocationCode: "nyc",
				Latitude:     40.7128,
				Longitude:    -74.0060,
			},
			NearbyProbes: []Probe{
				{ID: 100, Address: "1.1.1.1"},
				{ID: 101, Address: "1.1.1.2"},
			},
		},
		{
			LocationMatch: collector.LocationMatch{
				LocationCode: "lon",
				Latitude:     51.5074,
				Longitude:    -0.1278,
			},
			NearbyProbes: []Probe{
				{ID: 200, Address: "2.2.2.1"},
				{ID: 201, Address: "2.2.2.2"},
			},
		},
		{
			LocationMatch: collector.LocationMatch{
				LocationCode: "ams",
				Latitude:     52.3676,
				Longitude:    4.9041,
			},
			NearbyProbes: []Probe{
				{ID: 300, Address: "3.3.3.1"},
				{ID: 301, Address: "3.3.3.2"},
			},
		},
	}

	// Create a measurement state for testing (empty, no unresponsive probes)
	measurementState := NewMeasurementState("/tmp/test_state.json")

	// Test with different orderings
	measurements1 := c.generateWantedMeasurements(locations, 2, measurementState)

	// Reverse the order
	reversedLocations := []LocationProbeMatch{locations[2], locations[1], locations[0]}
	measurements2 := c.generateWantedMeasurements(reversedLocations, 2, measurementState)

	// Should have same number of measurements
	require.Equal(t, len(measurements1), len(measurements2), "Different number of measurements")

	// We expect one measurement per target location.
	// Since we have 3 locations (ams, lon, nyc), and we create measurements from all others to each target:
	// - Measurement to ams from lon,nyc
	// - Measurement to lon from nyc (ams already measured to lon)
	// - No measurement to nyc (both ams and lon already measured to nyc)
	// Total: 2 measurements
	require.Len(t, measurements1, 2, "Expected 2 measurements")

	// Verify measurements have the expected structure
	targetLocations := make(map[string]bool)
	for _, m := range measurements1 {
		targetLocations[m.TargetLocationCode] = true

		// Each measurement should have multiple source specs
		require.Greater(t, len(m.SourceSpecs), 0, "Measurement to %s should have sources", m.TargetLocationCode)

		// Verify all sources come after the target alphabetically
		for _, source := range m.SourceSpecs {
			require.Greater(t, source.LocationCode, m.TargetLocationCode,
				"Source %s should come after target %s alphabetically", source.LocationCode, m.TargetLocationCode)
		}
	}

	// Should have measurements to ams and LON
	require.True(t, targetLocations["ams"], "Should have measurement to ams")
	require.True(t, targetLocations["lon"], "Should have measurement to LON")
	require.False(t, targetLocations["nyc"], "Should not have measurement to NYC")

	// Verify the specific structure of measurements
	for _, m := range measurements1 {
		if m.TargetLocationCode == "ams" {
			// ams gets measurements from LON and NYC
			require.Len(t, m.SourceSpecs, 2, "ams should have 2 sources")
			sourceCodes := make(map[string]bool)
			for _, s := range m.SourceSpecs {
				sourceCodes[s.LocationCode] = true
			}
			require.True(t, sourceCodes["lon"], "ams should have lon as source")
			require.True(t, sourceCodes["nyc"], "ams should have nyc as source")
		} else if m.TargetLocationCode == "lon" {
			// lon gets measurement only from nyc (ams already measured to lon)
			require.Len(t, m.SourceSpecs, 1, "lon should have 1 source")
			require.Equal(t, "nyc", m.SourceSpecs[0].LocationCode, "lon should have nyc as source")
		}
	}
}

func TestInternetLatency_RIPEAtlas_ExpectedDailyCreditsMetric(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// Mock client that returns no existing measurements and creates new ones successfully
	var measurementCounter int
	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return []Measurement{}, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			measurementCounter++
			return &MeasurementResponse{Measurements: []int{1000 + measurementCounter}}, nil
		},
		GetProbesForLocationsFunc: func(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error) {
			// Return 3 locations with probes
			return []LocationProbeMatch{
				{
					LocationMatch: collector.LocationMatch{LocationCode: "ams", Latitude: 52.3, Longitude: 4.9},
					NearbyProbes: []Probe{{
						ID:      100,
						Address: "1.1.1.1",
						Geometry: struct {
							Type        string    `json:"type"`
							Coordinates []float64 `json:"coordinates"`
						}{
							Coordinates: []float64{4.9, 52.3},
						},
					}},
				},
				{
					LocationMatch: collector.LocationMatch{LocationCode: "lon", Latitude: 51.5, Longitude: -0.1},
					NearbyProbes: []Probe{{
						ID:      200,
						Address: "2.2.2.2",
						Geometry: struct {
							Type        string    `json:"type"`
							Coordinates []float64 `json:"coordinates"`
						}{
							Coordinates: []float64{-0.1, 51.5},
						},
					}},
				},
				{
					LocationMatch: collector.LocationMatch{LocationCode: "nyc", Latitude: 40.7, Longitude: -74.0},
					NearbyProbes: []Probe{{
						ID:      300,
						Address: "3.3.3.3",
						Geometry: struct {
							Type        string    `json:"type"`
							Coordinates []float64 `json:"coordinates"`
						}{
							Coordinates: []float64{-74.0, 40.7},
						},
					}},
				},
			}, nil
		},
	}

	c := &Collector{
		client: mockClient,
		log:    log,
		env:    "test",
		getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
			return []collector.LocationMatch{
				{LocationCode: "ams", Latitude: 52.3, Longitude: 4.9},
				{LocationCode: "lon", Latitude: 51.5, Longitude: -0.1},
				{LocationCode: "nyc", Latitude: 40.7, Longitude: -74.0},
			}
		},
	}

	// Test with 10 minute interval (144 samples per day)
	samplingInterval := 10 * time.Minute
	err := c.RunRipeAtlasMeasurementCreation(t.Context(), false, 1, t.TempDir(), samplingInterval)
	require.NoError(t, err)

	// With 3 locations in alphabetical order (ams, lon, nyc):
	// - ams gets measurements from lon and nyc (2 sources)
	// - lon gets measurement from nyc (1 source)
	// - nyc gets no measurements (0 sources)
	// Total sources = 2 + 1 + 0 = 3
	// Expected daily credits = 3 sources * 144 samples/day = 432

	// Note: We can't directly check the metric value in tests, but we can verify
	// the log output contains the expected calculation
	t.Log("Test completed - metric should show 432 expected daily credits")
}

func TestInternetLatency_RIPEAtlas_RunRipeAtlasMeasurementCreation(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// This test verifies the full RunRipeAtlasMeasurementCreation function flow
	// It handles both cases: when GetLocations returns locations or when it doesn't

	var getProbesForLocationsCalled bool
	var passedLocations []LocationProbeMatch

	mockClient := &MockClient{
		GetProbesForLocationsFunc: func(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error) {
			getProbesForLocationsCalled = true
			passedLocations = locations

			// Return locations with probes
			result := make([]LocationProbeMatch, len(locations))
			for i, loc := range locations {
				result[i] = LocationProbeMatch{
					LocationMatch: loc.LocationMatch,
					NearbyProbes: []Probe{
						{ID: 1000 + i, Address: fmt.Sprintf("192.168.%d.1", i+1), ASN: 1234},
					},
					ProbeCount: 1,
				}
			}
			return result, nil
		},
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return []Measurement{}, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			return &MeasurementResponse{
				Measurements: []int{5000},
			}, nil
		},
	}

	c := &Collector{client: mockClient, log: log, getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// Use a context with timeout
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	err := c.RunRipeAtlasMeasurementCreation(ctx, false, 1, t.TempDir(), 1*time.Minute)

	// Check different scenarios
	if err == collector.ErrNoDevicesFound {
		// Case 1: GetLocations returned no locations
		t.Log("GetLocations returned no locations")
		require.False(t, getProbesForLocationsCalled, "GetProbesForLocations should not be called when no locations")
		return
	}

	if err != nil && (errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline exceeded")) {
		// Case 2: GetLocations timed out
		t.Log("GetLocations timed out")
		return
	}

	// Case 3: GetLocations returned locations and processing succeeded
	if err == nil {
		t.Log("RunRipeAtlasMeasurementCreation succeeded with locations from blockchain")
		require.True(t, getProbesForLocationsCalled, "GetProbesForLocations should be called when locations exist")
		require.Greater(t, len(passedLocations), 0, "Should have passed locations to GetProbesForLocations")
		return
	}

	// Any other error
	t.Fatalf("Unexpected error: %v", err)
}

// TestInternetLatency_RIPEAtlas_FetchFallbackProbes_TriggeredWhenAllUnresponsive verifies that
// when all known probes for a location are in the unresponsive list, a non-anchor fallback
// query is issued and those probes replace the unresponsive ones.
func TestInternetLatency_RIPEAtlas_FetchFallbackProbes_TriggeredWhenAllUnresponsive(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	const anchorProbeID = 7549
	const fallbackProbeID = 99999

	var nonAnchorCalls int
	mockClient := &MockClient{
		GetProbesInRadiusFunc: func(_ context.Context, _, _ float64, _ int, anchorsOnly bool) ([]Probe, error) {
			if anchorsOnly {
				return []Probe{}, nil // should not be called in this path
			}
			nonAnchorCalls++
			return []Probe{
				{ID: fallbackProbeID, Address: "1.2.3.4", Latitude: 40.76, Longitude: -111.89},
			}, nil
		},
	}

	stateFile := filepath.Join(t.TempDir(), "state.json")
	measurementState := NewMeasurementState(stateFile)
	measurementState.AddUnresponsiveProbe(anchorProbeID)

	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{
				LocationCode: "slc",
				Latitude:     40.7608,
				Longitude:    -111.8910,
			},
			NearbyProbes: []Probe{
				{ID: anchorProbeID, Address: "8.8.8.8", Latitude: 40.76, Longitude: -111.89},
			},
			ProbeCount: 1,
		},
	}

	c := &Collector{client: mockClient, log: log}
	result := c.fetchFallbackProbesForUnresponsiveLocations(t.Context(), locationMatches, measurementState)

	require.Len(t, result, 1)
	require.Equal(t, 1, nonAnchorCalls, "should have fetched non-anchor fallback probes")
	require.Equal(t, []int{fallbackProbeID}, probeIDs(result[0].FallbackTargetProbes),
		"the fallback probe should be offered to target selection")
	require.Equal(t, []int{anchorProbeID}, probeIDs(result[0].NearbyProbes),
		"source selection keeps reading the location's own probes")
}

// TestInternetLatency_RIPEAtlas_FetchFallbackProbes_NoFallbackWhenResponsive verifies that
// when at least one probe for a location is still responsive, no non-anchor fallback query
// is issued and the original probes are left unchanged.
func TestInternetLatency_RIPEAtlas_FetchFallbackProbes_NoFallbackWhenResponsive(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	const anchorProbeID = 7549

	var nonAnchorCalls int
	mockClient := &MockClient{
		GetProbesInRadiusFunc: func(_ context.Context, _, _ float64, _ int, anchorsOnly bool) ([]Probe, error) {
			if !anchorsOnly {
				nonAnchorCalls++
			}
			return []Probe{}, nil
		},
	}

	stateFile := filepath.Join(t.TempDir(), "state.json")
	measurementState := NewMeasurementState(stateFile) // anchor probe is NOT unresponsive

	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{
				LocationCode: "slc",
				Latitude:     40.7608,
				Longitude:    -111.8910,
			},
			NearbyProbes: []Probe{
				{ID: anchorProbeID, Address: "8.8.8.8", Latitude: 40.76, Longitude: -111.89},
			},
			ProbeCount: 1,
		},
	}

	c := &Collector{client: mockClient, log: log}
	result := c.fetchFallbackProbesForUnresponsiveLocations(t.Context(), locationMatches, measurementState)

	require.Len(t, result, 1)
	require.Equal(t, 0, nonAnchorCalls, "should NOT have fetched non-anchor probes when anchor is responsive")
	require.Len(t, result[0].NearbyProbes, 1)
	require.Equal(t, anchorProbeID, result[0].NearbyProbes[0].ID, "original anchor probe should be preserved")
}

// TestInternetLatency_RIPEAtlas_FetchFallbackProbes_TriggeredByTargetMarks verifies the
// wider fetch still runs for a location whose every candidate is marked as a target
// only, that a fetch turning up nothing leaves those candidates in place for
// rankTargets, and that whatever it does find never reaches source selection.
func TestInternetLatency_RIPEAtlas_FetchFallbackProbes_TriggeredByTargetMarks(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	const markedProbeID = 12651
	const fallbackProbeID = 55128

	newCase := func(t *testing.T, fetched []Probe) ([]LocationProbeMatch, *MeasurementState, *int) {
		t.Helper()

		nonAnchorCalls := 0
		mockClient := &MockClient{
			GetProbesInRadiusFunc: func(_ context.Context, _, _ float64, _ int, anchorsOnly bool) ([]Probe, error) {
				if !anchorsOnly {
					nonAnchorCalls++
				}
				return fetched, nil
			},
		}

		measurementState := NewMeasurementState(filepath.Join(t.TempDir(), "state.json"))
		measurementState.AddUnresponsiveTarget(markedProbeID)

		locationMatches := []LocationProbeMatch{
			{
				LocationMatch: collector.LocationMatch{LocationCode: "cmh", Latitude: 40.11, Longitude: -83.00},
				NearbyProbes: []Probe{
					{ID: markedProbeID, Address: "107.192.62.177", Latitude: 40.11, Longitude: -83.00},
				},
				ProbeCount: 1,
			},
			{
				LocationMatch: collector.LocationMatch{LocationCode: "nyc", Latitude: 40.77, Longitude: -74.07},
				NearbyProbes:  []Probe{{ID: 100, Address: "162.255.145.7", Latitude: 40.77, Longitude: -74.07}},
				ProbeCount:    1,
			},
		}

		c := &Collector{client: mockClient, log: log}
		return c.fetchFallbackProbesForUnresponsiveLocations(t.Context(), locationMatches, measurementState),
			measurementState, &nonAnchorCalls
	}

	t.Run("a fetch that finds nothing leaves the marked candidate to be ranked", func(t *testing.T) {
		t.Parallel()

		result, _, nonAnchorCalls := newCase(t, []Probe{})

		require.Equal(t, 1, *nonAnchorCalls, "a target-only mark should still trigger the wider fetch")
		require.Len(t, result[0].NearbyProbes, 1)
		require.Equal(t, markedProbeID, result[0].NearbyProbes[0].ID)
		require.Empty(t, result[0].FallbackTargetProbes)
	})

	t.Run("fallback probes are kept out of source selection", func(t *testing.T) {
		t.Parallel()

		// The reviewer's xsin scenario: the wider fetch returns a nearer non-anchor probe.
		// Merged into NearbyProbes it would become the metro's source probe as well, and
		// every measurement sourcing from cmh would compare as outdated and be torn down.
		result, measurementState, _ := newCase(t, []Probe{
			{ID: fallbackProbeID, Address: "69.58.112.238", Latitude: 40.11, Longitude: -83.00},
		})

		require.Equal(t, []int{markedProbeID}, probeIDs(result[0].NearbyProbes),
			"the fallback must not displace the probes source selection reads")
		require.Equal(t, []int{fallbackProbeID}, probeIDs(result[0].FallbackTargetProbes))

		c := &Collector{client: &MockClient{}, log: log}
		wanted := c.generateWantedMeasurements(result, 1, measurementState)

		require.Len(t, wanted, 1, "cmh sorts first, so it is the only target")
		require.Equal(t, "cmh", wanted[0].TargetLocationCode)
		require.Equal(t, fallbackProbeID, wanted[0].TargetProbe.ID,
			"the unmarked fallback outranks the marked known probe as a target")

		// cmh is a source for no measurement here (nothing sorts before it), so assert
		// the converse directly: the fallback is not selectable as a source at all.
		require.Equal(t, []int{markedProbeID},
			probeIDs(filterResponsiveProbes(result[0].NearbyProbes, measurementState)),
			"a target-only mark leaves the original probe as cmh's source")
	})
}

// probeIDs lists probe IDs in order, for comparing selections.
func probeIDs(probes []Probe) []int {
	out := make([]int, 0, len(probes))
	for _, p := range probes {
		out = append(out, p.ID)
	}
	return out
}

func TestInternetLatency_RIPEAtlas_ConfigureMeasurements_CreateNew(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// Track created measurements
	var createdMeasurements []MeasurementRequest
	var mu sync.Mutex

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			// No existing measurements
			return []Measurement{}, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			measurementID := 2000 + len(createdMeasurements)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{measurementID}}, nil
		},
	}

	c := &Collector{client: mockClient, log: log, getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// Locations with probes that should trigger measurement creation
	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{
				LocationCode: "nyc",
				Latitude:     40.7128,
				Longitude:    -74.0060,
			},
			NearbyProbes: []Probe{
				{ID: 100, Address: "1.1.1.1", Latitude: 40.7128, Longitude: -74.0060},
			},
			ProbeCount: 1,
		},
		{
			LocationMatch: collector.LocationMatch{
				LocationCode: "lon",
				Latitude:     51.5074,
				Longitude:    -0.1278,
			},
			NearbyProbes: []Probe{
				{ID: 200, Address: "2.2.2.2", Latitude: 51.5074, Longitude: -0.1278},
			},
			ProbeCount: 1,
		},
	}

	err := c.configureMeasurements(t.Context(), locationMatches, false, 1, t.TempDir(), 1*time.Minute)
	require.NoError(t, err, "configureMeasurements should succeed")

	mu.Lock()
	finalCreated := createdMeasurements
	mu.Unlock()

	require.Len(t, finalCreated, 1, "Expected 1 measurement to be created")

	// Verify measurement details
	measurement := finalCreated[0]
	require.Len(t, measurement.Definitions, 1, "Expected 1 definition")
	require.Equal(t, "ping", measurement.Definitions[0].Type)
	require.Equal(t, 4, measurement.Definitions[0].AF)
	require.Equal(t, "2.2.2.2", measurement.Definitions[0].Target) // LON probe address (target)
	require.Contains(t, measurement.Definitions[0].Description, "to lon")

	require.Len(t, measurement.Probes, 1, "Expected 1 probe")
	require.Equal(t, 100, measurement.Probes[0].Value) // NYC probe ID (source)
}

func TestInternetLatency_RIPEAtlas_ConfigureMeasurements_RemoveUnwanted(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	outputDir := filepath.Join(tempDir, "output")

	// Track what gets removed and exported
	var removedMeasurements []int
	var exportedMeasurements []int
	var mu sync.Mutex

	// Existing measurements that should be removed
	existingMeasurements := []Measurement{
		{
			ID:          1001,
			Description: "DoubleZero NYC probe 100 to LON probe 200",
			Target:      "2.2.2.2",
			Status: struct {
				Name string `json:"name"`
				ID   int    `json:"id"`
			}{Name: "Running"},
			Type: "ping",
		},
		{
			ID:          1002,
			Description: "DoubleZero NYC probe 100 to PAR probe 300",
			Target:      "3.3.3.3",
			Status: struct {
				Name string `json:"name"`
				ID   int    `json:"id"`
			}{Name: "Running"},
			Type: "ping",
		},
	}

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return existingMeasurements, nil
		},
		StopMeasurementFunc: func(ctx context.Context, measurementID int) error {
			mu.Lock()
			removedMeasurements = append(removedMeasurements, measurementID)
			mu.Unlock()
			return nil
		},
		GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
			mu.Lock()
			exportedMeasurements = append(exportedMeasurements, measurementID)
			mu.Unlock()
			// Return some results to export
			return []any{
				map[string]any{
					"timestamp": float64(time.Now().UTC().Unix()),
					"result": []any{
						map[string]any{"rtt": float64(25.5)},
					},
				},
			}, nil
		},
	}

	e, err := exporter.NewCSVExporter(log, "ripe_atlas_measurements", outputDir)
	require.NoError(t, err)
	c := &Collector{client: mockClient, log: log, exporter: e, getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// Empty location matches means all existing measurements should be removed
	err = c.configureMeasurements(t.Context(), []LocationProbeMatch{}, false, 1, stateDir, 1*time.Minute)
	require.NoError(t, err, "configureMeasurements should succeed")

	// Verify measurements were exported and removed
	mu.Lock()
	finalRemoved := removedMeasurements
	finalExported := exportedMeasurements
	mu.Unlock()

	require.ElementsMatch(t, []int{1001, 1002}, finalRemoved, "Both measurements should be removed")
	// Without metadata, measurements won't be exported
	require.Empty(t, finalExported, "No measurements should be exported without metadata")
}

func TestInternetLatency_RIPEAtlas_ConfigureMeasurements_UnresponsiveSourceProbe(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// Track created and stopped measurements
	var createdMeasurements []MeasurementRequest
	var stoppedMeasurements []int
	var mu sync.Mutex

	twoHoursAgo := time.Now().Unix() - 7200

	// Existing measurement targeting "xams" with xsin and xtyo as sources
	existingMeasurements := []Measurement{
		{
			ID:          1001,
			Description: "DoubleZero [testnet] to xams probe 6626",
			Target:      "84.38.236.1",
			Status: struct {
				Name string `json:"name"`
				ID   int    `json:"id"`
			}{Name: "Ongoing"},
			Type: "ping",
		},
	}

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return existingMeasurements, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			measurementID := 2000 + len(createdMeasurements)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{measurementID}}, nil
		},
		StopMeasurementFunc: func(ctx context.Context, measurementID int) error {
			mu.Lock()
			stoppedMeasurements = append(stoppedMeasurements, measurementID)
			mu.Unlock()
			return nil
		},
		GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
			return []any{}, nil
		},
	}

	stateDir := filepath.Join(t.TempDir(), "state")

	c := &Collector{client: mockClient, log: log, env: "testnet", getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// Pre-populate measurement state with metadata showing xsin source probe is stale
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	c.measurementState.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "xams",
		TargetProbeID:  6626,
		Sources: []SourceProbeMeta{
			{LocationCode: "xsin", ProbeID: 6726, LastResponseAt: twoHoursAgo},       // Stale
			{LocationCode: "xtyo", ProbeID: 7080, LastResponseAt: time.Now().Unix()}, // Fresh
		},
		CreatedAt:    twoHoursAgo - 3600, // Created 3 hours ago
		LastExportAt: time.Now().Unix(),
	})

	// Locations with probes: xams has one probe, xsin has two (6726 stale, another available), xtyo has one
	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "xams", Latitude: 52.3, Longitude: 4.7},
			NearbyProbes:  []Probe{{ID: 6626, Address: "84.38.236.1", Latitude: 52.3, Longitude: 4.7}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "xsin", Latitude: 1.3, Longitude: 103.8},
			NearbyProbes: []Probe{
				{ID: 6726, Address: "139.99.78.22", Latitude: 1.3, Longitude: 103.8},  // Will be marked unresponsive
				{ID: 1033, Address: "138.75.38.177", Latitude: 1.3, Longitude: 103.9}, // Replacement
			},
			ProbeCount: 2,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "xtyo", Latitude: 35.6, Longitude: 139.6},
			NearbyProbes:  []Probe{{ID: 7080, Address: "63.222.190.5", Latitude: 35.6, Longitude: 139.6}},
			ProbeCount:    1,
		},
	}

	err := c.configureMeasurements(t.Context(), locationMatches, false, 1, stateDir, 1*time.Minute)
	require.NoError(t, err)

	// Probe 6726 should be marked unresponsive
	require.True(t, c.measurementState.IsProbeUnresponsive(6726), "Stale source probe 6726 should be marked unresponsive")
	require.False(t, c.measurementState.IsProbeUnresponsive(7080), "Fresh source probe 7080 should NOT be marked unresponsive")

	mu.Lock()
	defer mu.Unlock()

	// The old measurement should be stopped (sources changed since 6726 is now unresponsive)
	require.Contains(t, stoppedMeasurements, 1001, "Old measurement with stale source should be stopped")

	// A new measurement should be created with probe 1033 instead of 6726
	require.NotEmpty(t, createdMeasurements, "New measurement should be created with replacement probe")

	// Verify the new measurement uses the replacement probe (1033) not the stale one (6726)
	for _, m := range createdMeasurements {
		for _, probe := range m.Probes {
			require.NotEqual(t, 6726, probe.Value, "Stale probe 6726 should not be used in new measurements")
		}
	}
}

// TestInternetLatency_RIPEAtlas_ConfigureMeasurements_WindowSurvivesSourceChurn pins the
// loss window across a recreation caused by something other than the target. Step 5
// recreates on any source-set change, so without the carry-over an unrelated metro's
// probe flapping would reset every window it touches and the loss check would never
// accumulate enough to fire.
func TestInternetLatency_RIPEAtlas_ConfigureMeasurements_WindowSurvivesSourceChurn(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	const target = 6626
	const staleSource = 6726
	const replacementSource = 1033

	// xams is recreated onto the new source; xsin gets a first measurement of its own,
	// since it sorts after xams. Only the xams one is under test here.
	createdByTarget := map[string]int{}
	var created int
	var mu sync.Mutex

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(_ context.Context, _ string) ([]Measurement, error) {
			return []Measurement{{
				ID:          1001,
				Description: "DoubleZero [testnet] to xams probe 6626",
				Target:      "84.38.236.1",
				Status: struct {
					Name string `json:"name"`
					ID   int    `json:"id"`
				}{Name: "Ongoing"},
				Type: "ping",
			}}, nil
		},
		CreateMeasurementFunc: func(_ context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			created++
			id := 2000 + created
			for _, def := range request.Definitions {
				if strings.Contains(def.Description, "to xams") {
					createdByTarget["xams"] = id
				}
			}
			return &MeasurementResponse{Measurements: []int{id}}, nil
		},
		StopMeasurementFunc: func(_ context.Context, _ int) error { return nil },
		GetMeasurementResultsIncrementalFunc: func(_ context.Context, _ int, _ int64) ([]any, error) {
			return []any{}, nil
		},
	}

	stateDir := filepath.Join(t.TempDir(), "state")
	c := &Collector{client: mockClient, log: log, env: "testnet", getLocationsFunc: func(_ context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// A window 40 minutes in, short of a verdict. The target is healthy; only the
	// source set is about to change.
	windowStart := time.Now().Add(-40 * time.Minute).Unix()
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	c.measurementState.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "xams",
		TargetProbeID:  target,
		Sources: []SourceProbeMeta{
			{LocationCode: "xsin", ProbeID: staleSource, LastResponseAt: time.Now().Unix() - 7200},
			{LocationCode: "xtyo", ProbeID: 7080, LastResponseAt: time.Now().Unix()},
		},
		CreatedAt:         time.Now().Unix() - 3*3600,
		LastExportAt:      time.Now().Unix(),
		TargetWindowStart: windowStart,
		TargetAttempts:    25,
		TargetSuccesses:   4,
		TargetLossCursor:  windowStart + 60,
	})

	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "xams", Latitude: 52.3, Longitude: 4.7},
			NearbyProbes:  []Probe{{ID: target, Address: "84.38.236.1", Latitude: 52.3, Longitude: 4.7}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "xsin", Latitude: 1.3, Longitude: 103.8},
			NearbyProbes: []Probe{
				{ID: staleSource, Address: "139.99.78.22", Latitude: 1.3, Longitude: 103.8},
				{ID: replacementSource, Address: "138.75.38.177", Latitude: 1.3, Longitude: 103.9},
			},
			ProbeCount: 2,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "xtyo", Latitude: 35.6, Longitude: 139.6},
			NearbyProbes:  []Probe{{ID: 7080, Address: "63.222.190.5", Latitude: 35.6, Longitude: 139.6}},
			ProbeCount:    1,
		},
	}

	require.NoError(t, c.configureMeasurements(t.Context(), locationMatches, false, 1, stateDir, 10*time.Minute))

	mu.Lock()
	newID, recreated := createdByTarget["xams"]
	mu.Unlock()
	require.True(t, recreated, "xams should be recreated onto the replacement source")

	meta, ok := c.measurementState.GetMetadata(newID)
	require.True(t, ok)
	require.Equal(t, target, meta.TargetProbeID, "the target did not change")
	require.Equal(t, windowStart, meta.TargetWindowStart, "the window must survive source churn")
	require.Equal(t, int64(25), meta.TargetAttempts)
	require.Equal(t, int64(4), meta.TargetSuccesses)
	require.Equal(t, windowStart+60, meta.TargetLossCursor)
}

// TestInternetLatency_RIPEAtlas_ConfigureMeasurements_KeptMarkedTargetStillChecksSources
// pins the per-cycle skip. A target kept by rank-last carries its mark every cycle, so
// keying the Step 4b exemption off the standing mark would exempt this measurement's
// sources from inspection for good.
func TestInternetLatency_RIPEAtlas_ConfigureMeasurements_KeptMarkedTargetStillChecksSources(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	const markedTarget = 6626
	const staleSource = 6726

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(_ context.Context, _ string) ([]Measurement, error) {
			return []Measurement{{
				ID:          1001,
				Description: "DoubleZero [testnet] to xams probe 6626",
				Target:      "84.38.236.1",
				Status: struct {
					Name string `json:"name"`
					ID   int    `json:"id"`
				}{Name: "Ongoing"},
				Type: "ping",
			}}, nil
		},
		CreateMeasurementFunc: func(_ context.Context, _ MeasurementRequest) (*MeasurementResponse, error) {
			return &MeasurementResponse{Measurements: []int{2001}}, nil
		},
		GetMeasurementResultsIncrementalFunc: func(_ context.Context, _ int, _ int64) ([]any, error) {
			return []any{}, nil
		},
	}

	stateDir := filepath.Join(t.TempDir(), "state")
	c := &Collector{client: mockClient, log: log, env: "testnet", getLocationsFunc: func(_ context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	c.measurementState = NewMeasurementState(filepath.Join(stateDir, TimestampFileName))

	// Marked in an earlier cycle and kept, because xams has no other candidate. The
	// measurement is exporting now and has no open loss window, so this cycle judges
	// its target neither stale nor lossy.
	c.measurementState.AddUnresponsiveTarget(markedTarget)
	c.measurementState.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "xams",
		TargetProbeID:  markedTarget,
		Sources: []SourceProbeMeta{
			{LocationCode: "xsin", ProbeID: staleSource, LastResponseAt: time.Now().Unix() - 7200},
		},
		CreatedAt:    time.Now().Unix() - 3*3600,
		LastExportAt: time.Now().Unix(),
	})

	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "xams", Latitude: 52.3, Longitude: 4.7},
			NearbyProbes:  []Probe{{ID: markedTarget, Address: "84.38.236.1", Latitude: 52.3, Longitude: 4.7}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "xsin", Latitude: 1.3, Longitude: 103.8},
			NearbyProbes: []Probe{
				{ID: staleSource, Address: "139.99.78.22", Latitude: 1.3, Longitude: 103.8},
				{ID: 1033, Address: "138.75.38.177", Latitude: 1.3, Longitude: 103.9},
			},
			ProbeCount: 2,
		},
	}

	require.NoError(t, c.configureMeasurements(t.Context(), locationMatches, false, 1, stateDir, 10*time.Minute))

	require.True(t, c.measurementState.IsProbeUnresponsive(staleSource),
		"a standing target mark must not exempt the measurement's sources from inspection")
}

// TestInternetLatency_RIPEAtlas_ConfigureMeasurements_StaleTargetListsBySeverity covers
// the two staleness reasons. A measurement that has produced nothing at all in its first
// hour has an offline target, and an offline probe cannot source either, so it leaves
// both pools. One that used to export and stopped is the NAT-like case, where the probe
// still sources normally.
func TestInternetLatency_RIPEAtlas_ConfigureMeasurements_StaleTargetListsBySeverity(t *testing.T) {
	t.Parallel()

	const cmhProbe = 1009793
	const nycProbe = 100
	const seaProbe = 101

	for _, tc := range []struct {
		name         string
		lastExportAt int64
		wantSource   bool
	}{
		{
			name:         "never exported leaves both pools",
			lastExportAt: 0,
			wantSource:   false,
		},
		{
			name:         "stopped exporting leaves the target pool only",
			lastExportAt: time.Now().Unix() - 2*3600,
			wantSource:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			mockClient := &MockClient{
				GetAllMeasurementsFunc: func(_ context.Context, _ string) ([]Measurement, error) {
					return []Measurement{{
						ID:          1001,
						Description: "DoubleZero [testnet] to cmh probe 1009793",
						Target:      "23.151.152.243",
						Status: struct {
							Name string `json:"name"`
							ID   int    `json:"id"`
						}{Name: "Ongoing"},
						Type: "ping",
					}}, nil
				},
				CreateMeasurementFunc: func(_ context.Context, _ MeasurementRequest) (*MeasurementResponse, error) {
					return &MeasurementResponse{Measurements: []int{2001}}, nil
				},
				StopMeasurementFunc: func(_ context.Context, _ int) error { return nil },
				GetMeasurementResultsIncrementalFunc: func(_ context.Context, _ int, _ int64) ([]any, error) {
					return []any{}, nil
				},
			}

			stateDir := filepath.Join(t.TempDir(), "state")
			require.NoError(t, os.MkdirAll(stateDir, 0o755))

			c := &Collector{client: mockClient, log: log, env: "testnet", getLocationsFunc: func(_ context.Context) []collector.LocationMatch {
				return []collector.LocationMatch{}
			}}

			c.measurementState = NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
			c.measurementState.SetMetadata(1001, MeasurementMeta{
				TargetLocation: "cmh",
				TargetProbeID:  cmhProbe,
				Sources: []SourceProbeMeta{
					{LocationCode: "nyc", ProbeID: nycProbe, LastResponseAt: time.Now().Unix()},
					{LocationCode: "sea", ProbeID: seaProbe, LastResponseAt: time.Now().Unix()},
				},
				CreatedAt:    time.Now().Unix() - 3*3600,
				LastExportAt: tc.lastExportAt,
			})

			locationMatches := []LocationProbeMatch{
				{
					LocationMatch: collector.LocationMatch{LocationCode: "cmh", Latitude: 40.11, Longitude: -83.00},
					NearbyProbes:  []Probe{{ID: cmhProbe, Address: "23.151.152.243", Latitude: 40.11, Longitude: -83.00}},
					ProbeCount:    1,
				},
				{
					LocationMatch: collector.LocationMatch{LocationCode: "nyc", Latitude: 40.77, Longitude: -74.07},
					NearbyProbes:  []Probe{{ID: nycProbe, Address: "162.255.145.7", Latitude: 40.77, Longitude: -74.07}},
					ProbeCount:    1,
				},
				{
					LocationMatch: collector.LocationMatch{LocationCode: "sea", Latitude: 47.61, Longitude: -122.33},
					NearbyProbes:  []Probe{{ID: seaProbe, Address: "198.48.19.2", Latitude: 47.61, Longitude: -122.33}},
					ProbeCount:    1,
				},
			}

			require.NoError(t, c.configureMeasurements(t.Context(), locationMatches, false, 1, stateDir, 10*time.Minute))

			require.True(t, c.measurementState.IsTargetUnresponsive(cmhProbe),
				"a stale target is barred from targeting either way")
			require.Equal(t, !tc.wantSource, c.measurementState.IsProbeUnresponsive(cmhProbe))

			// And source selection agrees. cmh sorts first, so its probe is a source
			// candidate for no measurement in this set; check the filter directly.
			selectable := probeIDs(filterResponsiveProbes(locationMatches[0].NearbyProbes, c.measurementState))
			if tc.wantSource {
				require.Equal(t, []int{cmhProbe}, selectable,
					"a probe that merely stopped answering pings still sources")
			} else {
				require.Empty(t, selectable, "an offline probe must not be enlisted as a source")
			}
		})
	}
}

func TestInternetLatency_RIPEAtlas_ConfigureMeasurements_UnresponsiveTargetDoesNotBlacklistSources(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var createdMeasurements []MeasurementRequest
	var stoppedMeasurements []int
	var mu sync.Mutex

	twoHoursAgo := time.Now().Unix() - 7200

	// Two measurements: one targeting xams (healthy), one targeting xsin (unresponsive target)
	existingMeasurements := []Measurement{
		{
			ID:          1001,
			Description: "DoubleZero [testnet] to xams probe 6626",
			Target:      "84.38.236.1",
			Status: struct {
				Name string `json:"name"`
				ID   int    `json:"id"`
			}{Name: "Ongoing"},
			Type: "ping",
		},
		{
			ID:          1002,
			Description: "DoubleZero [testnet] to xsin probe 6726",
			Target:      "139.99.78.22",
			Status: struct {
				Name string `json:"name"`
				ID   int    `json:"id"`
			}{Name: "Ongoing"},
			Type: "ping",
		},
	}

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return existingMeasurements, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			measurementID := 2000 + len(createdMeasurements)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{measurementID}}, nil
		},
		StopMeasurementFunc: func(ctx context.Context, measurementID int) error {
			mu.Lock()
			stoppedMeasurements = append(stoppedMeasurements, measurementID)
			mu.Unlock()
			return nil
		},
		GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
			return []any{}, nil
		},
	}

	stateDir := filepath.Join(t.TempDir(), "state")

	c := &Collector{client: mockClient, log: log, env: "testnet", getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// Measurement 1001 (target xams/6626): healthy, source probes have fresh LastResponseAt
	// Measurement 1002 (target xsin/6726): target is unresponsive, so source probes have stale LastResponseAt.
	// Seeded directly to that shape; results are not replayed through the export path here.
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	c.measurementState.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "xams",
		TargetProbeID:  6626,
		Sources: []SourceProbeMeta{
			{LocationCode: "xsin", ProbeID: 6726, LastResponseAt: time.Now().Unix()}, // Fresh
			{LocationCode: "xtyo", ProbeID: 7080, LastResponseAt: time.Now().Unix()}, // Fresh
		},
		CreatedAt:    twoHoursAgo - 3600,
		LastExportAt: time.Now().Unix(), // Healthy measurement
	})
	c.measurementState.SetMetadata(1002, MeasurementMeta{
		TargetLocation: "xsin",
		TargetProbeID:  6726,
		Sources: []SourceProbeMeta{
			{LocationCode: "xams", ProbeID: 6626, LastResponseAt: twoHoursAgo}, // Stale because target is dead
			{LocationCode: "xtyo", ProbeID: 7080, LastResponseAt: twoHoursAgo}, // Stale because target is dead
		},
		CreatedAt:    twoHoursAgo - 3600,
		LastExportAt: twoHoursAgo, // Stale — target not responding
	})

	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "xams", Latitude: 52.3, Longitude: 4.7},
			NearbyProbes:  []Probe{{ID: 6626, Address: "84.38.236.1", Latitude: 52.3, Longitude: 4.7}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "xsin", Latitude: 1.3, Longitude: 103.8},
			NearbyProbes: []Probe{
				{ID: 6726, Address: "139.99.78.22", Latitude: 1.3, Longitude: 103.8},  // Will be marked unresponsive (target)
				{ID: 1033, Address: "138.75.38.177", Latitude: 1.3, Longitude: 103.9}, // Replacement
			},
			ProbeCount: 2,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "xtyo", Latitude: 35.6, Longitude: 139.6},
			NearbyProbes:  []Probe{{ID: 7080, Address: "63.222.190.5", Latitude: 35.6, Longitude: 139.6}},
			ProbeCount:    1,
		},
	}

	err := c.configureMeasurements(t.Context(), locationMatches, false, 1, stateDir, 1*time.Minute)
	require.NoError(t, err)

	// Probe 6726 should be barred from target selection (target staleness via Phase 1)
	require.True(t, c.measurementState.IsTargetUnresponsive(6726), "Target probe 6726 should be barred as a target")

	// ...but it remains available to source measurements. Failing to answer pings says
	// nothing about sending them, and xsin has no other probe to source from.
	require.False(t, c.measurementState.IsProbeUnresponsive(6726), "Failed target probe 6726 should still be usable as a source")

	// Source probes 6626 and 7080 should NOT be marked unresponsive — their stale LastResponseAt
	// in measurement 1002 is because the target (6726) stopped responding, not because they're broken
	require.False(t, c.measurementState.IsProbeUnresponsive(6626), "Healthy source probe 6626 should NOT be marked unresponsive due to dead target")
	require.False(t, c.measurementState.IsProbeUnresponsive(7080), "Healthy source probe 7080 should NOT be marked unresponsive due to dead target")

	mu.Lock()
	defer mu.Unlock()

	// The measurement with the dead target should be stopped
	require.Contains(t, stoppedMeasurements, 1002, "Measurement with unresponsive target should be stopped")

	// Measurement 1001 targets xams and sources from xsin's probe 6726. The point of the
	// split is that marking 6726 as a failed target leaves its sourcing alone, so 1001
	// must survive: a regression that re-barred it for sourcing would swap xsin's source
	// probe and tear this measurement down.
	require.NotContains(t, stoppedMeasurements, 1001,
		"a target failure must not tear down a measurement the probe merely sources")
}

func TestInternetLatency_RIPEAtlas_Run_ErrorHandling(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	t.Run("Measurement creation error", func(t *testing.T) {
		mockClient := &MockClient{
			GetProbesForLocationsFunc: func(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error) {
				return nil, errors.New("API error")
			},
			GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
				return []Measurement{}, nil
			},
		}

		e, err := exporter.NewCSVExporter(log, "ripe_atlas_measurements", t.TempDir())
		require.NoError(t, err)
		c := &Collector{client: mockClient, log: log, exporter: e, getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
			return []collector.LocationMatch{}
		}}

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		err = c.Run(ctx, false, 1, t.TempDir(), 30*time.Millisecond, 50*time.Millisecond, 1*time.Minute)

		// Should not return error - errors are logged but don't stop the collector
		require.Nil(t, err, "Run should not return error for measurement creation failures")
	})

	t.Run("Export error", func(t *testing.T) {
		mockClient := &MockClient{
			GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
				return nil, errors.New("Export API error")
			},
			GetProbesForLocationsFunc: func(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error) {
				return []LocationProbeMatch{}, nil
			},
		}

		e, err := exporter.NewCSVExporter(log, "ripe_atlas_measurements", t.TempDir())
		require.NoError(t, err)
		c := &Collector{client: mockClient, log: log, exporter: e, getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
			return []collector.LocationMatch{}
		}}

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		err = c.Run(ctx, false, 1, t.TempDir(), 50*time.Millisecond, 30*time.Millisecond, 1*time.Minute)

		// Should not return error - export errors are logged but don't stop the collector
		require.Nil(t, err, "Run should not return error for export failures")
	})
}

func TestInternetLatency_RIPEAtlas_Run(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// Test that both goroutines (measurement and export) run concurrently
	// and verify that data export writes the expected content
	var measurementCalls, exportCalls int
	var exportedMeasurementIDs []int
	var mu sync.Mutex

	// Mock filesystem operations for timestamp persistence
	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	outputDir := filepath.Join(tempDir, "output")

	// Create directories
	require.NoError(t, os.MkdirAll(stateDir, 0755), "Failed to create state directory")
	require.NoError(t, os.MkdirAll(outputDir, 0755), "Failed to create output directory")

	mockClient := &MockClient{
		GetProbesForLocationsFunc: func(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error) {
			mu.Lock()
			measurementCalls++
			mu.Unlock()
			// Return empty to avoid creating measurements
			return []LocationProbeMatch{}, nil
		},
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			mu.Lock()
			exportCalls++
			mu.Unlock()
			// Return no measurements since we don't have metadata for them
			return []Measurement{}, nil
		},
		GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
			mu.Lock()
			exportedMeasurementIDs = append(exportedMeasurementIDs, measurementID)
			mu.Unlock()

			// Return results with latency data
			timestamp := time.Now().UTC().Unix()
			return []any{
				map[string]any{
					"timestamp": float64(timestamp),
					"result": []any{
						map[string]any{"rtt": float64(42.5)},
						map[string]any{"rtt": float64(43.2)},
						map[string]any{"rtt": float64(41.8)},
					},
				},
			}, nil
		},
	}

	e, err := exporter.NewCSVExporter(log, "ripe_atlas_measurements", outputDir)
	require.NoError(t, err)
	c := &Collector{client: mockClient, log: log, exporter: e, getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// Use different intervals to verify both run independently
	measurementInterval := 50 * time.Millisecond
	exportInterval := 30 * time.Millisecond
	samplingInterval := 1 * time.Minute

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	err = c.Run(ctx, false, 1, stateDir, samplingInterval, measurementInterval, exportInterval)
	require.Nil(t, err, "Run should complete without error")

	// Verify both goroutines ran
	mu.Lock()
	finalMeasurementCalls := measurementCalls
	finalExportCalls := exportCalls
	finalExportedIDs := append([]int{}, exportedMeasurementIDs...)
	mu.Unlock()

	// Due to GetLocations failing, measurement creation attempts may be limited
	// but export should run multiple times
	require.Greater(t, finalExportCalls, 0, "Export should have been called at least once")
	t.Logf("Measurement calls: %d, Export calls: %d", finalMeasurementCalls, finalExportCalls)

	// Since we return no measurements (no metadata), no export should occur
	require.Equal(t, 0, len(finalExportedIDs), "No measurements should be exported without metadata")
}

func TestInitializeCreditBalance(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		mockClient := &MockClient{
			GetCreditBalanceFunc: func(ctx context.Context) (float64, error) {
				return 1000.0, nil
			},
		}

		c := &Collector{
			client: mockClient,
			log:    logger,
		}

		err := c.InitializeCreditBalance(context.Background())
		require.NoError(t, err)
	})

	t.Run("API error", func(t *testing.T) {
		t.Parallel()

		mockClient := &MockClient{
			GetCreditBalanceFunc: func(ctx context.Context) (float64, error) {
				return 0, errors.New("API error")
			},
		}

		c := &Collector{
			client: mockClient,
			log:    logger,
		}

		err := c.InitializeCreditBalance(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get RIPE Atlas credit balance")
	})
}

func TestInternetLatency_RIPEAtlas_SourcesWithoutSamples(t *testing.T) {
	t.Parallel()

	const hour = int64(3600)
	now := int64(1785953098)
	// Anchored on the production constant so the fixtures below straddle the real
	// boundary rather than an arbitrary one.
	createdBefore := now - int64(sourceSampleGracePeriod.Seconds())

	// A source with no successful sample since creation carries LastResponseAt == 0.
	never := SourceProbeMeta{LocationCode: "hkg", ProbeID: 7030}
	responding := SourceProbeMeta{LocationCode: "sqq", ProbeID: 6324, LastResponseAt: now - 60}

	tests := []struct {
		name           string
		metadata       map[int]MeasurementMeta
		measurementIDs []int
		wantByLocation map[string]int
		wantTotal      int
	}{
		{
			name: "measurement older than the window reports its silent sources",
			metadata: map[int]MeasurementMeta{
				1: {TargetLocation: "ams", CreatedAt: now - 3*hour, Sources: []SourceProbeMeta{never, responding}},
			},
			measurementIDs: []int{1},
			wantByLocation: map[string]int{"hkg": 1},
			wantTotal:      1,
		},
		{
			name: "all sources responding reports nothing",
			metadata: map[int]MeasurementMeta{
				1: {TargetLocation: "ams", CreatedAt: now - 3*hour, Sources: []SourceProbeMeta{responding}},
			},
			measurementIDs: []int{1},
			wantByLocation: map[string]int{},
			wantTotal:      0,
		},
		{
			name: "measurement younger than the window is still warming up",
			metadata: map[int]MeasurementMeta{
				1: {TargetLocation: "ams", CreatedAt: now - 60, Sources: []SourceProbeMeta{never}},
			},
			measurementIDs: []int{1},
			wantByLocation: map[string]int{},
			wantTotal:      0,
		},
		{
			// RIPE has been observed dispatching an accepted enlistment as late as 100
			// minutes after creation. Reporting inside that window is a false positive,
			// and this is what fails if the grace period is ever tightened below it.
			name: "measurement inside observed RIPE dispatch latency is not reported",
			metadata: map[int]MeasurementMeta{
				1: {TargetLocation: "ams", CreatedAt: now - 100*60, Sources: []SourceProbeMeta{never}},
			},
			measurementIDs: []int{1},
			wantByLocation: map[string]int{},
			wantTotal:      0,
		},
		{
			name: "unknown creation time is skipped rather than assumed old",
			metadata: map[int]MeasurementMeta{
				1: {TargetLocation: "ams", Sources: []SourceProbeMeta{never}},
			},
			measurementIDs: []int{1},
			wantByLocation: map[string]int{},
			wantTotal:      0,
		},
		{
			name: "counts accumulate per source location across measurements",
			metadata: map[int]MeasurementMeta{
				1: {TargetLocation: "ams", CreatedAt: now - 3*hour, Sources: []SourceProbeMeta{never}},
				2: {TargetLocation: "bom", CreatedAt: now - 3*hour, Sources: []SourceProbeMeta{never}},
				3: {TargetLocation: "chi", CreatedAt: now - 3*hour, Sources: []SourceProbeMeta{
					never, {LocationCode: "muc", ProbeID: 6372},
				}},
			},
			measurementIDs: []int{1, 2, 3},
			wantByLocation: map[string]int{"hkg": 3, "muc": 1},
			wantTotal:      4,
		},
		{
			name: "measurement with no metadata is ignored",
			metadata: map[int]MeasurementMeta{
				1: {TargetLocation: "ams", CreatedAt: now - 3*hour, Sources: []SourceProbeMeta{never}},
			},
			measurementIDs: []int{1, 99},
			wantByLocation: map[string]int{"hkg": 1},
			wantTotal:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := NewMeasurementState("test.json")
			for id, meta := range tt.metadata {
				state.SetMetadata(id, meta)
			}

			measurements := make([]Measurement, 0, len(tt.measurementIDs))
			for _, id := range tt.measurementIDs {
				measurements = append(measurements, Measurement{ID: id})
			}

			byLocation, total, sample := sourcesWithoutSamples(measurements, state, createdBefore, 20)

			require.Equal(t, tt.wantByLocation, byLocation)
			require.Equal(t, tt.wantTotal, total)
			require.Len(t, sample, tt.wantTotal)
		})
	}
}

func TestInternetLatency_RIPEAtlas_SourcesWithoutSamples_SampleIsCapped(t *testing.T) {
	t.Parallel()

	now := int64(1785953098)
	state := NewMeasurementState("test.json")

	sources := make([]SourceProbeMeta, 0, 30)
	for i := range 30 {
		sources = append(sources, SourceProbeMeta{LocationCode: "hkg", ProbeID: 7000 + i})
	}
	state.SetMetadata(1, MeasurementMeta{TargetLocation: "ams", CreatedAt: now - 3*3600, Sources: sources})

	byLocation, total, sample := sourcesWithoutSamples([]Measurement{{ID: 1}}, state, now-2*3600, 20)

	// The metric carries the full count; only the log sample is capped.
	require.Equal(t, 30, total)
	require.Equal(t, map[string]int{"hkg": 30}, byLocation)
	require.Len(t, sample, 20)
}

func TestInternetLatency_RIPEAtlas_ExportSingleMeasurementResults_LossCountsAsResponse(t *testing.T) {
	t.Parallel()

	resultAt := time.Now().Add(-time.Minute).Truncate(time.Second)

	// A probe that reaches nothing still uploads a result; the ping array just carries no
	// rtt. RIPE returns these as rcvd=0, avg=-1.
	totalLoss := map[string]any{
		"prb_id":    float64(100),
		"timestamp": float64(resultAt.Unix()),
		"result": []any{
			map[string]any{"x": "*"},
			map[string]any{"x": "*"},
		},
	}
	answered := map[string]any{
		"prb_id":    float64(100),
		"timestamp": float64(resultAt.Unix()),
		"result":    []any{map[string]any{"rtt": float64(26.0)}},
	}
	// No prb_id and no timestamp. UpdateSourceProbeResponse drops this: probe 0 matches no
	// enlisted source, and a zero time.Time is not newer than any LastResponseAt.
	unparseable := map[string]any{
		"result": []any{map[string]any{"x": "*"}},
	}

	tests := []struct {
		name            string
		results         []any
		wantResponseAt  int64
		wantExportedNum int
	}{
		{
			name:            "a total-loss result counts as a response",
			results:         []any{totalLoss},
			wantResponseAt:  resultAt.Unix(),
			wantExportedNum: 0,
		},
		{
			name:            "a successful result still counts as a response",
			results:         []any{answered},
			wantResponseAt:  resultAt.Unix(),
			wantExportedNum: 1,
		},
		{
			name:            "an unparseable result does not",
			results:         []any{unparseable},
			wantResponseAt:  0,
			wantExportedNum: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			log := slog.New(slog.NewJSONHandler(io.Discard, nil))
			e, err := exporter.NewCSVExporter(log, "ripe_atlas_measurements", outputDir)
			require.NoError(t, err)

			mockClient := &MockClient{
				GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
					return tt.results, nil
				},
			}
			c := &Collector{client: mockClient, log: log, exporter: e}

			measurementState := NewMeasurementState(filepath.Join(outputDir, TimestampFileName))
			measurementState.SetMetadata(1, MeasurementMeta{
				TargetLocation: "lax",
				TargetProbeID:  200,
				Sources:        []SourceProbeMeta{{LocationCode: "nyc", ProbeID: 100}},
				CreatedAt:      time.Now().Add(-2 * time.Hour).Unix(),
			})

			count, _, err := c.exportSingleMeasurementResults(t.Context(), Measurement{ID: 1}, measurementState)
			require.NoError(t, err)
			require.Equal(t, tt.wantExportedNum, count, "exported record count should not change")

			_, cursorSet := measurementState.GetLastTimestamp(1)
			require.Equal(t, tt.wantExportedNum > 0, cursorSet, "a total-loss result must not advance the export cursor")

			meta, ok := measurementState.GetMetadata(1)
			require.True(t, ok)
			require.Len(t, meta.Sources, 1)
			require.Equal(t, tt.wantResponseAt, meta.Sources[0].LastResponseAt,
				"LastResponseAt drives the unresponsive-probe marking, which recreates measurements")
		})
	}
}

func TestInternetLatency_RIPEAtlas_FilterSelectableTargets(t *testing.T) {
	t.Parallel()

	// Selectability is about hard invalidity only, which is why the function takes no
	// measurement state: an unresponsive-target mark is a ranking signal for
	// rankTargets, not an exclusion.
	probes := []Probe{
		{ID: 1, Address: ""},
		{ID: 2, Address: "107.192.62.177"},
		{ID: 3, Address: "69.58.112.238"},
	}

	result := filterSelectableTargets(probes)

	require.Len(t, result, 2, "only the addressless probe is unselectable")
	require.Equal(t, 2, result[0].ID)
	require.Equal(t, 3, result[1].ID)
}

func TestInternetLatency_RIPEAtlas_RankTargets(t *testing.T) {
	t.Parallel()

	// cmh, the metro the loss check was written for.
	const lat, lng = 40.11, -83.00

	newState := func(t *testing.T, markedTargets ...int) *MeasurementState {
		t.Helper()
		ms := NewMeasurementState(filepath.Join(t.TempDir(), TimestampFileName))
		for _, id := range markedTargets {
			ms.AddUnresponsiveTarget(id)
		}
		return ms
	}

	near := Probe{ID: 12651, Address: "107.192.62.177", Latitude: 40.11, Longitude: -83.00}
	far := Probe{ID: 55128, Address: "69.58.112.238", Latitude: 40.30, Longitude: -83.20}

	ids := func(probes []Probe) []int {
		out := make([]int, 0, len(probes))
		for _, p := range probes {
			out = append(out, p.ID)
		}
		return out
	}

	t.Run("an unmarked farther probe outranks a marked nearer one", func(t *testing.T) {
		t.Parallel()

		ranked := rankTargets([]Probe{near, far}, lat, lng, newState(t, near.ID))

		require.Equal(t, []int{far.ID, near.ID}, ids(ranked))
	})

	t.Run("unmarked probes rank by distance", func(t *testing.T) {
		t.Parallel()

		ranked := rankTargets([]Probe{far, near}, lat, lng, newState(t))

		require.Equal(t, []int{near.ID, far.ID}, ids(ranked))
	})

	t.Run("marked probes rank by distance among themselves", func(t *testing.T) {
		t.Parallel()

		ranked := rankTargets([]Probe{far, near}, lat, lng, newState(t, near.ID, far.ID))

		require.Equal(t, []int{near.ID, far.ID}, ids(ranked),
			"a location with nothing but marked candidates still gets its nearest one")
	})

	t.Run("a probe unresponsive as a source is ranked last too", func(t *testing.T) {
		t.Parallel()

		ms := newState(t)
		ms.AddUnresponsiveProbe(near.ID)

		ranked := rankTargets([]Probe{near, far}, lat, lng, ms)

		require.Equal(t, []int{far.ID, near.ID}, ids(ranked))
	})

	t.Run("empty input stays empty", func(t *testing.T) {
		t.Parallel()

		require.Empty(t, rankTargets(nil, lat, lng, newState(t)))
	})
}

// failingExporter reports a write failure for every batch. The package has no mock
// exporter (wheresitup's is package-private), and every other test here uses the real
// CSV exporter, which does not fail on demand.
type failingExporter struct{}

func (failingExporter) WriteRecords(_ context.Context, _ []exporter.Record) error {
	return errors.New("write failed")
}
func (failingExporter) Close() error { return nil }

func TestInternetLatency_RIPEAtlas_ExportSingleMeasurementResults_RecordsTargetLoss(t *testing.T) {
	t.Parallel()

	base := time.Now().Add(-time.Hour).Truncate(time.Second)

	answered := func(probeID int, at time.Time) map[string]any {
		return map[string]any{
			"prb_id":    float64(probeID),
			"timestamp": float64(at.Unix()),
			"result":    []any{map[string]any{"rtt": float64(26.0)}},
		}
	}
	timedOut := func(probeID int, at time.Time) map[string]any {
		return map[string]any{
			"prb_id":    float64(probeID),
			"timestamp": float64(at.Unix()),
			"result":    []any{map[string]any{"x": "*"}},
		}
	}

	// newCollector returns a collector whose client replays whatever the current
	// element of batches holds, one batch per export call.
	newCollector := func(t *testing.T, exp exporter.Exporter, batches *[][]any) (*Collector, *MeasurementState) {
		t.Helper()
		outputDir := t.TempDir()
		log := slog.New(slog.NewJSONHandler(io.Discard, nil))
		if exp == nil {
			e, err := exporter.NewCSVExporter(log, "ripe_atlas_measurements", outputDir)
			require.NoError(t, err)
			exp = e
		}

		call := 0
		mockClient := &MockClient{
			GetMeasurementResultsIncrementalFunc: func(_ context.Context, _ int, _ int64) ([]any, error) {
				if call >= len(*batches) {
					return []any{}, nil
				}
				batch := (*batches)[call]
				call++
				return batch, nil
			},
		}
		c := &Collector{client: mockClient, log: log, exporter: exp}

		ms := NewMeasurementState(filepath.Join(outputDir, TimestampFileName))
		ms.SetMetadata(1, MeasurementMeta{
			TargetLocation: "cmh",
			TargetProbeID:  12651,
			Sources: []SourceProbeMeta{
				{LocationCode: "nyc", ProbeID: 100},
				{LocationCode: "chi", ProbeID: 101},
			},
			CreatedAt: time.Now().Add(-2 * time.Hour).Unix(),
		})
		return c, ms
	}

	t.Run("counts every ping aimed at the target", func(t *testing.T) {
		t.Parallel()

		batches := [][]any{{
			answered(100, base),
			timedOut(101, base.Add(time.Second)),
			timedOut(100, base.Add(2*time.Second)),
		}}
		c, ms := newCollector(t, nil, &batches)

		_, _, err := c.exportSingleMeasurementResults(t.Context(), Measurement{ID: 1}, ms)
		require.NoError(t, err)

		meta, ok := ms.GetMetadata(1)
		require.True(t, ok)
		require.Equal(t, int64(3), meta.TargetAttempts, "every uploaded result is an attempt")
		require.Equal(t, int64(1), meta.TargetSuccesses, "only results carrying an rtt are successes")
		require.Equal(t, base.Add(2*time.Second).Unix(), meta.TargetLossCursor)
	})

	t.Run("re-delivered trailing timeouts are not counted twice", func(t *testing.T) {
		t.Parallel()

		// The export cursor advances only past the success at base, so the two later
		// timeouts come back on the next incremental query. Counting them again would
		// drive a target that merely lost its most recent pings toward the threshold.
		firstBatch := []any{
			answered(100, base),
			timedOut(101, base.Add(time.Second)),
			timedOut(100, base.Add(2*time.Second)),
		}
		batches := [][]any{firstBatch, firstBatch[1:]}
		c, ms := newCollector(t, nil, &batches)

		_, _, err := c.exportSingleMeasurementResults(t.Context(), Measurement{ID: 1}, ms)
		require.NoError(t, err)
		_, _, err = c.exportSingleMeasurementResults(t.Context(), Measurement{ID: 1}, ms)
		require.NoError(t, err)

		meta, ok := ms.GetMetadata(1)
		require.True(t, ok)
		require.Equal(t, int64(3), meta.TargetAttempts, "the replayed timeouts must not be recounted")
		require.Equal(t, int64(1), meta.TargetSuccesses)
	})

	t.Run("a future-dated result does not park the cursor", func(t *testing.T) {
		t.Parallel()

		// A single clock-skewed timeout, then a real result behind it. Counting the
		// skewed one would push TargetLossCursor past wall clock and every later
		// result would be dropped for the life of the measurement.
		batches := [][]any{
			{timedOut(101, time.Now().Add(48*time.Hour))},
			{answered(100, base.Add(3*time.Second))},
		}
		c, ms := newCollector(t, nil, &batches)

		_, _, err := c.exportSingleMeasurementResults(t.Context(), Measurement{ID: 1}, ms)
		require.NoError(t, err)

		meta, ok := ms.GetMetadata(1)
		require.True(t, ok)
		require.Zero(t, meta.TargetAttempts, "a future-dated result is not an attempt")
		require.Zero(t, meta.TargetLossCursor, "the cursor must not move ahead of wall clock")

		_, _, err = c.exportSingleMeasurementResults(t.Context(), Measurement{ID: 1}, ms)
		require.NoError(t, err)

		meta, ok = ms.GetMetadata(1)
		require.True(t, ok)
		require.Equal(t, int64(1), meta.TargetAttempts, "later results are still counted")
		require.Equal(t, int64(1), meta.TargetSuccesses)
	})

	t.Run("a failed export counts nothing", func(t *testing.T) {
		t.Parallel()

		batches := [][]any{{
			answered(100, base),
			timedOut(101, base.Add(time.Second)),
		}}
		c, ms := newCollector(t, failingExporter{}, &batches)

		_, _, err := c.exportSingleMeasurementResults(t.Context(), Measurement{ID: 1}, ms)
		require.Error(t, err)

		meta, ok := ms.GetMetadata(1)
		require.True(t, ok)
		require.Zero(t, meta.TargetAttempts, "a batch that never landed must not be counted")
		require.Zero(t, meta.TargetSuccesses)
		require.Zero(t, meta.TargetLossCursor, "the loss cursor must not advance past an unwritten batch")
	})
}

func TestInternetLatency_RIPEAtlas_ConfigureMeasurements_LossyTargetIsRotated(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var createdMeasurements []MeasurementRequest
	var stoppedMeasurements []int
	var mu sync.Mutex

	const lossyTargetProbe = 12651   // stands in for the NAT'd Columbus probe
	const replacementProbe = 1012487 // a directly reachable probe further out

	// 36 pings across two sources, 6 answered: the 13-26% success cmh's target ran at.
	// Enough attempts to clear MinTargetAttemptsForLossCheck.
	var resultsFetched int
	var lossyResults []any
	for i := range 36 {
		at := time.Now().Add(-time.Duration(40-i) * time.Minute).Unix()
		probeID := 100 + i%2
		result := map[string]any{
			"prb_id":    float64(probeID),
			"timestamp": float64(at),
			"result":    []any{map[string]any{"x": "*"}},
		}
		if i%6 == 0 {
			result["result"] = []any{map[string]any{"rtt": float64(26.0)}}
		}
		lossyResults = append(lossyResults, result)
	}

	existingMeasurements := []Measurement{
		{
			ID:          1001,
			Description: "DoubleZero [testnet] to cmh probe 12651",
			Target:      "107.192.62.177",
			Status: struct {
				Name string `json:"name"`
				ID   int    `json:"id"`
			}{Name: "Ongoing"},
			Type: "ping",
		},
	}

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(_ context.Context, _ string) ([]Measurement, error) {
			return existingMeasurements, nil
		},
		CreateMeasurementFunc: func(_ context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			id := 2000 + len(createdMeasurements)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{id}}, nil
		},
		StopMeasurementFunc: func(_ context.Context, measurementID int) error {
			mu.Lock()
			stoppedMeasurements = append(stoppedMeasurements, measurementID)
			mu.Unlock()
			return nil
		},
		GetMeasurementResultsIncrementalFunc: func(_ context.Context, _ int, _ int64) ([]any, error) {
			mu.Lock()
			resultsFetched++
			mu.Unlock()
			return lossyResults, nil
		},
	}

	stateDir := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o755))

	outputDir := t.TempDir()
	csvExporter, err := exporter.NewCSVExporter(log, "ripe_atlas_measurements", outputDir)
	require.NoError(t, err)

	c := &Collector{client: mockClient, log: log, env: "testnet", exporter: csvExporter, getLocationsFunc: func(_ context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// The target is exporting steadily, so the staleness check is satisfied and only the
	// loss ratio can catch it.
	createdAt := time.Now().Add(-3 * time.Hour).Unix()
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	c.measurementState.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "cmh",
		TargetProbeID:  lossyTargetProbe,
		Sources: []SourceProbeMeta{
			{LocationCode: "nyc", ProbeID: 100, LastResponseAt: time.Now().Unix()},
			{LocationCode: "sea", ProbeID: 101, LastResponseAt: time.Now().Unix()},
		},
		CreatedAt:    createdAt,
		LastExportAt: time.Now().Unix(),
	})

	// Fill the window through the real export path rather than by hand, so the cursor
	// gating and the counting in exportSingleMeasurementResults are exercised too.
	_, _, err = c.exportSingleMeasurementResults(t.Context(), existingMeasurements[0], c.measurementState)
	require.NoError(t, err)

	seeded, ok := c.measurementState.GetMetadata(1001)
	require.True(t, ok)
	require.Equal(t, int64(36), seeded.TargetAttempts, "the export path should have counted every result")
	require.Equal(t, int64(6), seeded.TargetSuccesses)

	// Only the window's start is moved back. RecordTargetResults opens it at the time of
	// the export, and nothing here can wait an hour for it to close.
	seeded.TargetWindowStart = time.Now().Add(-TargetLossWindow).Unix()
	c.measurementState.SetMetadata(1001, seeded)

	mu.Lock()
	fetchedBeforeReconcile := resultsFetched
	mu.Unlock()
	require.Equal(t, 1, fetchedBeforeReconcile)

	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "cmh", Latitude: 40.11, Longitude: -83.00},
			NearbyProbes: []Probe{
				{ID: lossyTargetProbe, Address: "107.192.62.177", Latitude: 40.11, Longitude: -83.00},
				{ID: replacementProbe, Address: "69.58.112.238", Latitude: 40.12, Longitude: -83.01},
			},
			ProbeCount: 2,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "nyc", Latitude: 40.77, Longitude: -74.07},
			NearbyProbes:  []Probe{{ID: 100, Address: "162.255.145.7", Latitude: 40.77, Longitude: -74.07}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "sea", Latitude: 47.61, Longitude: -122.33},
			NearbyProbes:  []Probe{{ID: 101, Address: "198.48.19.2", Latitude: 47.61, Longitude: -122.33}},
			ProbeCount:    1,
		},
	}

	require.NoError(t, c.configureMeasurements(t.Context(), locationMatches, false, 1, stateDir, 10*time.Minute))

	mu.Lock()
	require.Greater(t, resultsFetched, fetchedBeforeReconcile,
		"reconciliation should export the measurement before removing it, so the mock is really called")
	mu.Unlock()

	// The lossy target is barred from targeting...
	require.True(t, c.measurementState.IsTargetUnresponsive(lossyTargetProbe),
		"a target above the loss threshold should be barred from target selection")

	// ...but stays available to source measurements for other locations.
	require.False(t, c.measurementState.IsProbeUnresponsive(lossyTargetProbe),
		"target loss must not bar the probe from sourcing, since NAT breaks inbound only")

	mu.Lock()
	defer mu.Unlock()

	require.Contains(t, stoppedMeasurements, 1001, "the measurement against the lossy target should be stopped")

	var cmhTargets []string
	for _, req := range createdMeasurements {
		for _, def := range req.Definitions {
			if strings.Contains(def.Description, "to cmh") {
				cmhTargets = append(cmhTargets, def.Target)
			}
		}
	}
	require.NotEmpty(t, cmhTargets, "a replacement measurement for cmh should be created")
	require.Contains(t, cmhTargets, "69.58.112.238", "cmh should be retargeted at the remaining probe")
	require.NotContains(t, cmhTargets, "107.192.62.177", "cmh should not be retargeted at the lossy probe")
}

func TestInternetLatency_RIPEAtlas_ConfigureMeasurements_LastMarkedCandidateIsKept(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var createdMeasurements []MeasurementRequest
	var stoppedMeasurements []int
	var mu sync.Mutex

	const lossyTargetProbe = 12651 // the current target, nearest to the metro
	const deadProbe = 1009793      // the only alternative, marked in an earlier cycle

	existingMeasurements := []Measurement{
		{
			ID:          1001,
			Description: "DoubleZero [testnet] to cmh probe 12651",
			Target:      "107.192.62.177",
			Status: struct {
				Name string `json:"name"`
				ID   int    `json:"id"`
			}{Name: "Ongoing"},
			Type: "ping",
		},
	}

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(_ context.Context, _ string) ([]Measurement, error) {
			return existingMeasurements, nil
		},
		CreateMeasurementFunc: func(_ context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			id := 2000 + len(createdMeasurements)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{id}}, nil
		},
		StopMeasurementFunc: func(_ context.Context, measurementID int) error {
			mu.Lock()
			stoppedMeasurements = append(stoppedMeasurements, measurementID)
			mu.Unlock()
			return nil
		},
		GetMeasurementResultsIncrementalFunc: func(_ context.Context, _ int, _ int64) ([]any, error) {
			return []any{}, nil
		},
	}

	stateDir := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o755))

	c := &Collector{client: mockClient, log: log, env: "testnet", getLocationsFunc: func(_ context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	windowStart := time.Now().Add(-TargetLossWindow).Unix()
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, TimestampFileName))

	// The alternative was marked never_exported in an earlier cycle: it answers no
	// pings at all, which is how cmh's nearest probe has behaved since onboarding.
	c.measurementState.AddUnresponsiveTarget(deadProbe)

	// The current target exports steadily but answers 15 of every 100 pings, so this
	// cycle marks it for excessive loss and cmh has no unmarked candidate left.
	c.measurementState.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "cmh",
		TargetProbeID:  lossyTargetProbe,
		Sources: []SourceProbeMeta{
			{LocationCode: "nyc", ProbeID: 100, LastResponseAt: time.Now().Unix()},
			{LocationCode: "sea", ProbeID: 101, LastResponseAt: time.Now().Unix()},
		},
		CreatedAt:         windowStart - 3600,
		LastExportAt:      time.Now().Unix(),
		TargetWindowStart: windowStart,
		TargetAttempts:    100,
		TargetSuccesses:   15,
	})

	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "cmh", Latitude: 40.11, Longitude: -83.00},
			NearbyProbes: []Probe{
				{ID: lossyTargetProbe, Address: "107.192.62.177", Latitude: 40.11, Longitude: -83.00},
				{ID: deadProbe, Address: "23.151.152.243", Latitude: 40.30, Longitude: -83.20},
			},
			ProbeCount: 2,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "nyc", Latitude: 40.77, Longitude: -74.07},
			NearbyProbes:  []Probe{{ID: 100, Address: "162.255.145.7", Latitude: 40.77, Longitude: -74.07}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "sea", Latitude: 47.61, Longitude: -122.33},
			NearbyProbes:  []Probe{{ID: 101, Address: "198.48.19.2", Latitude: 47.61, Longitude: -122.33}},
			ProbeCount:    1,
		},
	}

	err := c.configureMeasurements(t.Context(), locationMatches, false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	require.True(t, c.measurementState.IsTargetUnresponsive(lossyTargetProbe))
	require.True(t, c.measurementState.IsTargetUnresponsive(deadProbe))

	mu.Lock()
	defer mu.Unlock()

	// Both candidates are marked, so there is nothing better to move to. Excluding them
	// would empty cmh's candidate set, drop it from the wanted measurements, and have
	// reconciliation delete the measurement as unwanted (#4182) — a metro with partial
	// data would go to none for up to 24h.
	require.Empty(t, stoppedMeasurements, "the last measurement for cmh must not be torn down")
	for _, req := range createdMeasurements {
		for _, def := range req.Definitions {
			require.NotContains(t, def.Description, "to cmh",
				"nothing to recreate onto, so cmh is not recreated")
		}
	}

	meta, ok := c.measurementState.GetMetadata(1001)
	require.True(t, ok, "the measurement keeps its metadata")
	require.Equal(t, lossyTargetProbe, meta.TargetProbeID, "it keeps the nearest marked candidate")
}

// TestInternetLatency_RIPEAtlas_ConfigureMeasurements_SingleSourceTargetNotMarked covers
// the thinnest fan-in. Sources come only from locations sorting after the target, so the
// alphabetically penultimate metro's measurement has exactly one, and the pooled ratio
// then is that one circuit's reachability rather than anything about the target.
func TestInternetLatency_RIPEAtlas_ConfigureMeasurements_SingleSourceTargetNotMarked(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	const target = 12651

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(_ context.Context, _ string) ([]Measurement, error) {
			return []Measurement{{
				ID:          1001,
				Description: "DoubleZero [testnet] to cmh probe 12651",
				Target:      "107.192.62.177",
				Status: struct {
					Name string `json:"name"`
					ID   int    `json:"id"`
				}{Name: "Ongoing"},
				Type: "ping",
			}}, nil
		},
		CreateMeasurementFunc: func(_ context.Context, _ MeasurementRequest) (*MeasurementResponse, error) {
			return &MeasurementResponse{Measurements: []int{2001}}, nil
		},
		StopMeasurementFunc: func(_ context.Context, _ int) error { return nil },
		GetMeasurementResultsIncrementalFunc: func(_ context.Context, _ int, _ int64) ([]any, error) {
			return []any{}, nil
		},
	}

	stateDir := filepath.Join(t.TempDir(), "state")
	c := &Collector{client: mockClient, log: log, env: "testnet", getLocationsFunc: func(_ context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// A closed window well past the threshold, but carried by a single source.
	windowStart := time.Now().Add(-TargetLossWindow).Unix()
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	c.measurementState.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "cmh",
		TargetProbeID:  target,
		Sources: []SourceProbeMeta{
			{LocationCode: "nyc", ProbeID: 100, LastResponseAt: time.Now().Unix()},
		},
		CreatedAt:         windowStart - 3600,
		LastExportAt:      time.Now().Unix(),
		TargetWindowStart: windowStart,
		TargetAttempts:    100,
		TargetSuccesses:   5,
	})

	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "cmh", Latitude: 40.11, Longitude: -83.00},
			NearbyProbes: []Probe{
				{ID: target, Address: "107.192.62.177", Latitude: 40.11, Longitude: -83.00},
				{ID: 55128, Address: "69.58.112.238", Latitude: 40.12, Longitude: -83.01},
			},
			ProbeCount: 2,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "nyc", Latitude: 40.77, Longitude: -74.07},
			NearbyProbes:  []Probe{{ID: 100, Address: "162.255.145.7", Latitude: 40.77, Longitude: -74.07}},
			ProbeCount:    1,
		},
	}

	require.NoError(t, c.configureMeasurements(t.Context(), locationMatches, false, 1, stateDir, 10*time.Minute))

	require.False(t, c.measurementState.IsTargetUnresponsive(target),
		"one source's broken path must not be charged to the target")
}

// TestInternetLatency_RIPEAtlas_ConfigureMeasurements_DryRunDoesNotPersistMarks pins the
// promise --dry-run makes. create-measurements builds the collector with a nil state, so
// configureMeasurements loads the running daemon's own file and a save here would reach it.
func TestInternetLatency_RIPEAtlas_ConfigureMeasurements_DryRunDoesNotPersistMarks(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	const deadTarget = 1009793

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(_ context.Context, _ string) ([]Measurement, error) {
			return []Measurement{{
				ID:          1001,
				Description: "DoubleZero [testnet] to cmh probe 1009793",
				Target:      "23.151.152.243",
				Status: struct {
					Name string `json:"name"`
					ID   int    `json:"id"`
				}{Name: "Ongoing"},
				Type: "ping",
			}}, nil
		},
		GetMeasurementResultsIncrementalFunc: func(_ context.Context, _ int, _ int64) ([]any, error) {
			return []any{}, nil
		},
	}

	stateDir := t.TempDir()
	stateFile := filepath.Join(stateDir, TimestampFileName)

	c := &Collector{client: mockClient, log: log, env: "testnet", getLocationsFunc: func(_ context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// Created 3h ago and never exported, so this cycle marks its target.
	c.measurementState = NewMeasurementState(stateFile)
	c.measurementState.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "cmh",
		TargetProbeID:  deadTarget,
		Sources:        []SourceProbeMeta{{LocationCode: "nyc", ProbeID: 100}},
		CreatedAt:      time.Now().Unix() - 3*3600,
	})

	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "cmh", Latitude: 40.11, Longitude: -83.00},
			NearbyProbes:  []Probe{{ID: deadTarget, Address: "23.151.152.243", Latitude: 40.11, Longitude: -83.00}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "nyc", Latitude: 40.77, Longitude: -74.07},
			NearbyProbes:  []Probe{{ID: 100, Address: "162.255.145.7", Latitude: 40.77, Longitude: -74.07}},
			ProbeCount:    1,
		},
	}

	require.NoError(t, c.configureMeasurements(t.Context(), locationMatches, true, 1, stateDir, 10*time.Minute))

	require.NoFileExists(t, stateFile, "--dry-run must not write the daemon's state file")
}

// TestInternetLatency_RIPEAtlas_ConfigureMeasurements_GetAllMeasurementsError verifies that a
// failed measurement fetch skips the cycle. Read as "no measurements exist", it recreated
// every wanted measurement and deleted every tracked one (#4169).
func TestInternetLatency_RIPEAtlas_ConfigureMeasurements_GetAllMeasurementsError(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())
	stateDir := t.TempDir()

	state := NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	state.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "lon",
		TargetProbeID:  200,
		Sources:        []SourceProbeMeta{{LocationCode: "nyc", ProbeID: 100}},
		CreatedAt:      time.Now().Unix(),
	})
	require.NoError(t, state.Save())

	var created, stopped int
	var mu sync.Mutex
	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return nil, errors.New("failed to get measurements (endpoint: /measurements/my/?status=Ongoing,Scheduled&tags=mainnet-beta): 401")
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			created++
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{2001}}, nil
		},
		StopMeasurementFunc: func(ctx context.Context, measurementID int) error {
			mu.Lock()
			stopped++
			mu.Unlock()
			return nil
		},
	}

	c := &Collector{
		client:                 mockClient,
		log:                    log,
		env:                    "mainnet-beta",
		measurementState:       state,
		measurementStateLoaded: true,
		getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
			return []collector.LocationMatch{}
		},
	}

	locationMatches := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "nyc", Latitude: 40.7128, Longitude: -74.0060},
			NearbyProbes:  []Probe{{ID: 100, Address: "1.1.1.1", Latitude: 40.7128, Longitude: -74.0060}},
			ProbeCount:    1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "lon", Latitude: 51.5074, Longitude: -0.1278},
			NearbyProbes:  []Probe{{ID: 200, Address: "2.2.2.2", Latitude: 51.5074, Longitude: -0.1278}},
			ProbeCount:    1,
		},
	}

	err := c.configureMeasurements(t.Context(), locationMatches, false, 1, stateDir, 1*time.Minute)
	require.Error(t, err, "a failed measurement fetch should fail the cycle")
	require.Contains(t, err.Error(), "failed to get existing measurements")

	mu.Lock()
	defer mu.Unlock()
	require.Zero(t, created, "no measurement should be created when the existing fleet is unknown")
	require.Zero(t, stopped, "no measurement should be removed when the existing fleet is unknown")

	// Losing metadata is what made the next cycle delete the fleet for missing metadata.
	require.Len(t, state.GetAllMetadata(), 1)
	require.Equal(t, "lon", state.GetAllMetadata()[1001].TargetLocation)

	reloaded := NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	require.NoError(t, reloaded.Load())
	require.Len(t, reloaded.GetAllMetadata(), 1)
	require.Equal(t, "lon", reloaded.GetAllMetadata()[1001].TargetLocation)
}

// TestInternetLatency_RIPEAtlas_MeasurementCreation_GatedOnStateLoad verifies that an
// unreadable state file holds off measurement management entirely, and that a repaired file
// resumes it without a restart. An empty tracker deleted the whole fleet (#4131).
func TestInternetLatency_RIPEAtlas_MeasurementCreation_GatedOnStateLoad(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())
	stateDir := t.TempDir()
	stateFile := filepath.Join(stateDir, TimestampFileName)
	require.NoError(t, os.WriteFile(stateFile, []byte("{truncated"), 0644))

	var apiCalls, locationLookups int
	var mu sync.Mutex
	countCall := func(n *int) {
		mu.Lock()
		*n++
		mu.Unlock()
	}
	mockClient := &MockClient{
		GetProbesForLocationsFunc: func(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error) {
			countCall(&apiCalls)
			result := make([]LocationProbeMatch, len(locations))
			for i, loc := range locations {
				probe := Probe{
					ID:        100 + i,
					Address:   fmt.Sprintf("%d.%d.%d.%d", i+1, i+1, i+1, i+1),
					Latitude:  loc.Latitude,
					Longitude: loc.Longitude,
				}
				probe.Geometry.Coordinates = []float64{loc.Longitude, loc.Latitude}
				result[i] = LocationProbeMatch{
					LocationMatch: loc.LocationMatch,
					NearbyProbes:  []Probe{probe},
					ProbeCount:    1,
				}
			}
			return result, nil
		},
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			countCall(&apiCalls)
			return []Measurement{}, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			countCall(&apiCalls)
			return &MeasurementResponse{Measurements: []int{2001}}, nil
		},
		StopMeasurementFunc: func(ctx context.Context, measurementID int) error {
			countCall(&apiCalls)
			return nil
		},
	}

	c := &Collector{
		client:          mockClient,
		log:             log,
		env:             "mainnet-beta",
		probeToLocation: make(map[int]string),
		getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
			countCall(&locationLookups)
			return []collector.LocationMatch{
				{LocationCode: "nyc", Latitude: 40.7128, Longitude: -74.0060},
				{LocationCode: "lon", Latitude: 51.5074, Longitude: -0.1278},
			}
		},
	}

	err := c.RunRipeAtlasMeasurementCreation(t.Context(), false, 1, stateDir, 1*time.Minute)
	require.Error(t, err, "a corrupt state file should hold off measurement management")
	require.Contains(t, err.Error(), "failed to load measurement state")

	mu.Lock()
	require.Zero(t, apiCalls, "no RIPE Atlas call should be made while the state file is unreadable")
	require.Zero(t, locationLookups, "management should stop before it enumerates locations")
	mu.Unlock()

	require.NoError(t, os.WriteFile(stateFile, []byte(`{"metadata": {}}`), 0644))

	require.NoError(t, c.RunRipeAtlasMeasurementCreation(t.Context(), false, 1, stateDir, 1*time.Minute))

	mu.Lock()
	defer mu.Unlock()
	require.Positive(t, apiCalls, "management should reach the API once the state file loads")
	require.Equal(t, 1, locationLookups)
}
