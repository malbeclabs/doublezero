package ripeatlas

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/stretchr/testify/require"
)

// cloudTestLocationMatches returns the two cloud regions as location matches carrying their
// pinned probes.
func cloudTestLocationMatches() []LocationProbeMatch {
	return []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "eu-west-1", Latitude: 53.3498, Longitude: -6.2603},
			NearbyProbes: []Probe{
				{ID: 1000441, Address: "10.0.0.1", Latitude: 53.3498, Longitude: -6.2603},
			},
			ProbeCount: 1,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "us-east-1", Latitude: 39.0438, Longitude: -77.4874},
			NearbyProbes: []Probe{
				{ID: 1000731, Address: "10.0.0.2", Latitude: 39.0438, Longitude: -77.4874},
			},
			ProbeCount: 1,
		},
	}
}

func TestInternetLatency_RIPEAtlas_TargetAddress_ExchangeUsesTargetProbeAddress(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := NewCollector(log, nil, "testnet", func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	})

	measurementState := NewMeasurementState(filepath.Join(t.TempDir(), TimestampFileName))
	wanted := c.generateWantedMeasurements(exchangeTestLocations(), 1, measurementState)

	require.Len(t, wanted, 2)
	require.Equal(t, "3.3.3.1", wanted[0].TargetAddress, "exchange mode pings the target probe address")
	require.Equal(t, wanted[0].TargetProbe.Address, wanted[0].TargetAddress)
	require.Equal(t, "2.2.2.1", wanted[1].TargetAddress)
	require.Equal(t, wanted[1].TargetProbe.Address, wanted[1].TargetAddress)
}

func TestInternetLatency_RIPEAtlas_TargetAddress_CloudUsesNodeFilePingTarget(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var createdMeasurements []MeasurementRequest
	var mu sync.Mutex

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return []Measurement{}, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			measurementID := 3000 + len(createdMeasurements)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{measurementID}}, nil
		},
	}

	stateDir := t.TempDir()
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudTestNodes())

	measurementState := NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	wanted := c.generateWantedMeasurements(cloudTestLocationMatches(), 1, measurementState)

	require.Len(t, wanted, 1, "two regions give one measurement")
	require.Equal(t, "eu-west-1", wanted[0].TargetLocationCode)
	require.Equal(t, "3.248.0.0", wanted[0].TargetAddress, "cloud mode pings the node file ping_target")
	require.NotEqual(t, wanted[0].TargetProbe.Address, wanted[0].TargetAddress,
		"the far-end probe address must not be the ping target")

	err := c.configureMeasurements(t.Context(), cloudTestLocationMatches(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, createdMeasurements, 1)
	require.Equal(t, "3.248.0.0", createdMeasurements[0].Definitions[0].Target,
		"the created measurement must ping the fixed region address")
}

func TestInternetLatency_RIPEAtlas_TargetAddress_CloudSkipsLocationWithoutPingTarget(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	nodes := cloudTestNodes()
	nodes[0].PingTarget = ""

	c := newCloudTestCollector(t, log, &MockClient{}, "mainnet-beta", nodes)

	measurementState := NewMeasurementState(filepath.Join(t.TempDir(), TimestampFileName))
	wanted := c.generateWantedMeasurements(cloudTestLocationMatches(), 1, measurementState)

	require.Empty(t, wanted, "a target region with no ping target yields no measurement")
}

func TestInternetLatency_RIPEAtlas_TargetAddress_ChangeRecreatesMeasurement(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var createdMeasurements []MeasurementRequest
	var stoppedMeasurements []int
	var mu sync.Mutex

	existing := []Measurement{
		{
			ID:          1001,
			Description: "DoubleZero [testnet] to ams probe 300",
			Target:      "3.3.3.1",
			Status: struct {
				Name string `json:"name"`
				ID   int    `json:"id"`
			}{Name: "Ongoing"},
			Type: "ping",
		},
	}

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return existing, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			measurementID := 4000 + len(createdMeasurements)
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

	stateDir := t.TempDir()
	c := &Collector{client: mockClient, log: log, env: "testnet", getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// The stored address differs from the probe's current address, so ams is stale.
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	c.measurementState.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "ams",
		TargetProbeID:  300,
		TargetAddress:  "3.3.3.9",
		Sources: []SourceProbeMeta{
			{LocationCode: "lon", ProbeID: 200, LastResponseAt: time.Now().Unix()},
			{LocationCode: "nyc", ProbeID: 100, LastResponseAt: time.Now().Unix()},
		},
		CreatedAt:    time.Now().Unix() - 60,
		LastExportAt: time.Now().Unix(),
	})

	err := c.configureMeasurements(t.Context(), exchangeTestLocations(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.Contains(t, stoppedMeasurements, 1001, "a changed target address must recreate the measurement")
	require.NotEmpty(t, createdMeasurements)
	require.Equal(t, "3.3.3.1", createdMeasurements[0].Definitions[0].Target)
}

func TestInternetLatency_RIPEAtlas_TargetAddress_EmptyStoredValueIsNotAChange(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var stoppedMeasurements []int
	var mu sync.Mutex

	existing := []Measurement{
		{
			ID:          1001,
			Description: "DoubleZero [testnet] to ams probe 300",
			Target:      "3.3.3.1",
			Status: struct {
				Name string `json:"name"`
				ID   int    `json:"id"`
			}{Name: "Ongoing"},
			Type: "ping",
		},
	}

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, env string) ([]Measurement, error) {
			return existing, nil
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

	stateDir := t.TempDir()
	c := &Collector{client: mockClient, log: log, env: "testnet", getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// State written before the field existed carries no address.
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	c.measurementState.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "ams",
		TargetProbeID:  300,
		Sources: []SourceProbeMeta{
			{LocationCode: "lon", ProbeID: 200, LastResponseAt: time.Now().Unix()},
			{LocationCode: "nyc", ProbeID: 100, LastResponseAt: time.Now().Unix()},
		},
		CreatedAt:    time.Now().Unix() - 60,
		LastExportAt: time.Now().Unix(),
	})

	err := c.configureMeasurements(t.Context(), exchangeTestLocations(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.Empty(t, stoppedMeasurements, "an unset stored target address must not force a recreation")
}

func TestInternetLatency_RIPEAtlas_TargetAddress_ProbeChangeStillRecreatesMeasurement(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var createdMeasurements []MeasurementRequest
	var stoppedMeasurements []int
	var mu sync.Mutex

	existing := []Measurement{
		{
			ID:          1001,
			Description: "DoubleZero [testnet] to ams probe 301",
			Target:      "3.3.3.2",
			Status: struct {
				Name string `json:"name"`
				ID   int    `json:"id"`
			}{Name: "Ongoing"},
			Type: "ping",
		},
	}

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, tag string) ([]Measurement, error) {
			return existing, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			measurementID := 5000 + len(createdMeasurements)
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

	stateDir := t.TempDir()
	c := &Collector{client: mockClient, log: log, env: "testnet", getLocationsFunc: func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	}}

	// State written before the address field existed, so only the target probe can differ:
	// 301 is stored, 300 is the nearest responsive probe for ams.
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	c.measurementState.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "ams",
		TargetProbeID:  301,
		Sources: []SourceProbeMeta{
			{LocationCode: "lon", ProbeID: 200, LastResponseAt: time.Now().Unix()},
			{LocationCode: "nyc", ProbeID: 100, LastResponseAt: time.Now().Unix()},
		},
		CreatedAt:    time.Now().Unix() - 60,
		LastExportAt: time.Now().Unix(),
	})

	err := c.configureMeasurements(t.Context(), exchangeTestLocations(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.Contains(t, stoppedMeasurements, 1001, "a changed target probe must recreate the measurement")

	var amsCreated []string
	for _, m := range createdMeasurements {
		if m.Definitions[0].Target == "3.3.3.1" {
			amsCreated = append(amsCreated, m.Definitions[0].Description)
		}
	}
	require.Equal(t, []string{"DoubleZero [testnet] to ams probe 300"}, amsCreated,
		"ams is recreated exactly once, against its current probe")
}
