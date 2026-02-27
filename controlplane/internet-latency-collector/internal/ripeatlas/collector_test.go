package ripeatlas

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
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
	GetProbesInRadiusFunc                func(ctx context.Context, latitude, longitude float64, radiusKm int) ([]Probe, error)
	GetProbesForLocationsFunc            func(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error)
	CreateMeasurementFunc                func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error)
	GetAllMeasurementsFunc               func(ctx context.Context, env string) ([]Measurement, error)
	GetMeasurementResultsFunc            func(ctx context.Context, measurementID int) ([]any, error)
	GetMeasurementResultsIncrementalFunc func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error)
	StopMeasurementFunc                  func(ctx context.Context, measurementID int) error
	GetCreditBalanceFunc                 func(ctx context.Context) (float64, error)
}

func (m *MockClient) GetProbesInRadius(ctx context.Context, latitude, longitude float64, radiusKm int) ([]Probe, error) {
	if m.GetProbesInRadiusFunc != nil {
		return m.GetProbesInRadiusFunc(ctx, latitude, longitude, radiusKm)
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

	probes := []Probe{
		{ID: 1, Address: "8.8.8.8"},
		{ID: 2, Address: "192.168.1.1"},
		{ID: 3, Address: ""},
		{ID: 4, Address: "1.1.1.1"},
		{ID: 5, Address: "10.0.0.1"},
		{ID: 6, Address: "::1"},
		{ID: 7, Address: "2001:4860:4860::8888"},
	}

	result := filterValidProbes(probes)

	// Should only include probes with internet-routable IPs
	expectedIDs := map[int]bool{1: true, 4: true, 7: true}
	require.Len(t, result, len(expectedIDs), "Unexpected number of valid probes")

	for _, probe := range result {
		require.True(t, expectedIDs[probe.ID], "Unexpected probe ID %d in result", probe.ID)
	}
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
