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

// cloudMeasurement is the eu-west-1 measurement cloud mode creates for cloudTestNodes.
func cloudMeasurement() Measurement {
	return Measurement{
		ID:          8001,
		Description: "DoubleZero Cloud [mainnet-beta] to eu-west-1 target 3.248.0.0",
		Target:      "3.248.0.0",
		Status: struct {
			Name string `json:"name"`
			ID   int    `json:"id"`
		}{Name: "Ongoing"},
		Type: "ping",
	}
}

func TestInternetLatency_RIPEAtlas_CloudBlame_StaleMeasurementBlamesNoProbe(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var stoppedMeasurements []int
	var mu sync.Mutex

	twoHoursAgo := time.Now().Unix() - 7200

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, tag string) ([]Measurement, error) {
			return []Measurement{cloudMeasurement()}, nil
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
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudTestNodes())

	// The measurement has exported nothing for two hours, and its stored target probe is
	// also the live source probe for us-east-1 in this same run.
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, CloudTimestampFileName))
	c.measurementState.SetMetadata(8001, MeasurementMeta{
		TargetLocation: "eu-west-1",
		TargetProbeID:  1000731,
		TargetAddress:  "3.248.0.0",
		Sources: []SourceProbeMeta{
			{LocationCode: "us-east-1", ProbeID: 1000731, LastResponseAt: time.Now().Unix()},
		},
		CreatedAt:    twoHoursAgo - 3600,
		LastExportAt: twoHoursAgo,
	})

	err := c.configureMeasurements(t.Context(), cloudTestLocationMatches(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	require.False(t, c.measurementState.IsProbeUnresponsive(1000731),
		"cloud mode must not blame the stored target probe, it is a source elsewhere")
	require.Empty(t, c.measurementState.GetUnresponsiveProbes(),
		"a quiet cloud measurement must mark no probe unresponsive")

	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, stoppedMeasurements, "the measurement still matches what is wanted")
}

func TestInternetLatency_RIPEAtlas_CloudBlame_NeverStartedSourceIsMarkedAfterGrace(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, tag string) ([]Measurement, error) {
			return []Measurement{cloudMeasurement()}, nil
		},
		GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
			return []any{}, nil
		},
	}

	stateDir := t.TempDir()
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudTestNodes())

	// The measurement is exporting, but its one source has never produced a sample and was
	// created well past the grace period.
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, CloudTimestampFileName))
	c.measurementState.SetMetadata(8001, MeasurementMeta{
		TargetLocation: "eu-west-1",
		TargetProbeID:  1000731,
		TargetAddress:  "3.248.0.0",
		Sources: []SourceProbeMeta{
			{LocationCode: "us-east-1", ProbeID: 1000731, LastResponseAt: 0},
		},
		CreatedAt:    time.Now().Unix() - 10800,
		LastExportAt: time.Now().Unix(),
	})

	err := c.configureMeasurements(t.Context(), cloudTestLocationMatches(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	require.True(t, c.measurementState.IsProbeUnresponsive(1000731),
		"a cloud source that has delivered nothing since creation must be blacklisted")
}

func TestInternetLatency_RIPEAtlas_CloudBlame_NeverStartedSourceKeptInsideGrace(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, tag string) ([]Measurement, error) {
			return []Measurement{cloudMeasurement()}, nil
		},
		GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
			return []any{}, nil
		},
	}

	stateDir := t.TempDir()
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudTestNodes())

	// Created 90 minutes ago, inside the two-hour grace, so the source is still warming up.
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, CloudTimestampFileName))
	c.measurementState.SetMetadata(8001, MeasurementMeta{
		TargetLocation: "eu-west-1",
		TargetProbeID:  1000731,
		TargetAddress:  "3.248.0.0",
		Sources: []SourceProbeMeta{
			{LocationCode: "us-east-1", ProbeID: 1000731, LastResponseAt: 0},
		},
		CreatedAt:    time.Now().Unix() - 5400,
		LastExportAt: time.Now().Unix(),
	})

	err := c.configureMeasurements(t.Context(), cloudTestLocationMatches(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	require.Empty(t, c.measurementState.GetUnresponsiveProbes(),
		"a cloud source inside the grace period must not be blacklisted")
}

func TestInternetLatency_RIPEAtlas_CloudBlame_ExchangeKeepsNeverStartedSource(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, tag string) ([]Measurement, error) {
			return []Measurement{{
				ID:          1001,
				Description: "DoubleZero [testnet] to ams probe 300",
				Target:      "3.3.3.1",
				Status: struct {
					Name string `json:"name"`
					ID   int    `json:"id"`
				}{Name: "Ongoing"},
				Type: "ping",
			}}, nil
		},
		GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
			return []any{}, nil
		},
	}

	stateDir := t.TempDir()
	c := NewCollector(log, nil, "testnet", func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	})
	c.client = mockClient

	c.measurementState = NewMeasurementState(filepath.Join(stateDir, TimestampFileName))
	c.measurementState.SetMetadata(1001, MeasurementMeta{
		TargetLocation: "ams",
		TargetProbeID:  300,
		TargetAddress:  "3.3.3.1",
		Sources: []SourceProbeMeta{
			{LocationCode: "lon", ProbeID: 200, LastResponseAt: 0},
			{LocationCode: "nyc", ProbeID: 100, LastResponseAt: 0},
		},
		CreatedAt:    time.Now().Unix() - 10800,
		LastExportAt: time.Now().Unix(),
	})

	err := c.configureMeasurements(t.Context(), exchangeTestLocations(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	require.Empty(t, c.measurementState.GetUnresponsiveProbes(),
		"exchange mode still waits for an export cycle to populate a source's last response")
}

func TestInternetLatency_RIPEAtlas_CloudBlame_TargetRegionNeedsNoResponsiveProbe(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := newCloudTestCollector(t, log, &MockClient{}, "mainnet-beta", cloudTestNodes())

	measurementState := NewMeasurementState(filepath.Join(t.TempDir(), CloudTimestampFileName))
	measurementState.AddUnresponsiveProbe(1000441)

	wanted := c.generateWantedMeasurements(cloudTestLocationMatches(), 1, measurementState)

	require.Len(t, wanted, 1, "nothing pings the target region, so its own probe need not be live")
	require.Equal(t, "eu-west-1", wanted[0].TargetLocationCode)
	require.Equal(t, "3.248.0.0", wanted[0].TargetAddress)
	require.Len(t, wanted[0].SourceSpecs, 1)
	require.Equal(t, 1000731, wanted[0].SourceSpecs[0].Probe.ID)
}

func TestInternetLatency_RIPEAtlas_CloudBlame_ExchangeTargetStillNeedsAResponsiveProbe(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := NewCollector(log, nil, "testnet", func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	})

	measurementState := NewMeasurementState(filepath.Join(t.TempDir(), TimestampFileName))
	measurementState.AddUnresponsiveProbe(300)
	measurementState.AddUnresponsiveProbe(301)

	wanted := c.generateWantedMeasurements(exchangeTestLocations(), 1, measurementState)

	require.Len(t, wanted, 1, "ams has no live probe, so it drops out as a target")
	require.Equal(t, "lon", wanted[0].TargetLocationCode)
}

// cloudFleetNodes gives three regions, each with a spare probe to rotate to.
func cloudFleetNodes() []CloudNode {
	return []CloudNode{
		{Code: "eu-west-1", Cloud: "aws", Latitude: 53.3498, Longitude: -6.2603,
			AtlasProbeIDs: []int{1000441, 1000442}, PingTarget: "3.248.0.0"},
		{Code: "us-east-1", Cloud: "aws", Latitude: 39.0438, Longitude: -77.4874,
			AtlasProbeIDs: []int{1000731, 1000732}, PingTarget: "34.192.0.54"},
		{Code: "us-west-2", Cloud: "aws", Latitude: 45.8399, Longitude: -119.7006,
			AtlasProbeIDs: []int{1000901, 1000902}, PingTarget: "52.32.0.0"},
	}
}

func cloudFleetLocations() []LocationProbeMatch {
	return []LocationProbeMatch{
		{LocationMatch: collector.LocationMatch{LocationCode: "eu-west-1", Latitude: 53.3498, Longitude: -6.2603}},
		{LocationMatch: collector.LocationMatch{LocationCode: "us-east-1", Latitude: 39.0438, Longitude: -77.4874}},
		{LocationMatch: collector.LocationMatch{LocationCode: "us-west-2", Latitude: 45.8399, Longitude: -119.7006}},
	}
}

// cloudFleetMeasurements is every measurement three regions produce: eu-west-1 sourced from the
// other two, us-east-1 sourced from us-west-2.
func cloudFleetMeasurements() []Measurement {
	ongoing := struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	}{Name: "Ongoing"}

	return []Measurement{
		{
			ID:          8001,
			Description: "DoubleZero Cloud [mainnet-beta] to eu-west-1 target 3.248.0.0",
			Target:      "3.248.0.0",
			Status:      ongoing,
			Type:        "ping",
		},
		{
			ID:          8002,
			Description: "DoubleZero Cloud [mainnet-beta] to us-east-1 target 34.192.0.54",
			Target:      "34.192.0.54",
			Status:      ongoing,
			Type:        "ping",
		},
	}
}

func TestInternetLatency_RIPEAtlas_CloudBlame_FleetThatExportedNothingIsNotTornDown(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var createdMeasurements []MeasurementRequest
	var stoppedMeasurements []int
	var mu sync.Mutex

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, tag string) ([]Measurement, error) {
			return cloudFleetMeasurements(), nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			measurementID := 9000 + len(createdMeasurements)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{measurementID}}, nil
		},
		StopMeasurementFunc: func(ctx context.Context, measurementID int) error {
			mu.Lock()
			stoppedMeasurements = append(stoppedMeasurements, measurementID)
			mu.Unlock()
			return nil
		},
	}

	stateDir := t.TempDir()
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudFleetNodes())

	// A first deployment three hours in: every measurement was created in one batch, nothing
	// has landed yet, so every source still reads zero.
	threeHoursAgo := time.Now().Unix() - 10800
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, CloudTimestampFileName))
	c.measurementState.SetMetadata(8001, MeasurementMeta{
		TargetLocation: "eu-west-1",
		TargetAddress:  "3.248.0.0",
		Sources: []SourceProbeMeta{
			{LocationCode: "us-east-1", ProbeID: 1000731, LastResponseAt: 0},
			{LocationCode: "us-west-2", ProbeID: 1000901, LastResponseAt: 0},
		},
		CreatedAt: threeHoursAgo,
	})
	c.measurementState.SetMetadata(8002, MeasurementMeta{
		TargetLocation: "us-east-1",
		TargetAddress:  "34.192.0.54",
		Sources: []SourceProbeMeta{
			{LocationCode: "us-west-2", ProbeID: 1000901, LastResponseAt: 0},
		},
		CreatedAt: threeHoursAgo,
	})

	err := c.configureMeasurements(t.Context(), cloudFleetLocations(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	require.Empty(t, c.measurementState.GetUnresponsiveProbes(),
		"a fleet that has exported nothing names no source probe as the cause")

	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, stoppedMeasurements, "a fleet that has exported nothing must not be torn down")
	require.Empty(t, createdMeasurements, "the measurements already running are still the wanted ones")
}

func TestInternetLatency_RIPEAtlas_CloudBlame_LiveMeasurementBlamesItsDarkSource(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, tag string) ([]Measurement, error) {
			return cloudFleetMeasurements(), nil
		},
	}

	stateDir := t.TempDir()
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudFleetNodes())

	// eu-west-1 is exporting on us-east-1's samples while us-west-2 has delivered nothing to
	// it. us-east-1 has only us-west-2 to draw on, so it exports nothing at all.
	now := time.Now().Unix()
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, CloudTimestampFileName))
	c.measurementState.SetMetadata(8001, MeasurementMeta{
		TargetLocation: "eu-west-1",
		TargetAddress:  "3.248.0.0",
		Sources: []SourceProbeMeta{
			{LocationCode: "us-east-1", ProbeID: 1000731, LastResponseAt: now},
			{LocationCode: "us-west-2", ProbeID: 1000901, LastResponseAt: 0},
		},
		CreatedAt:    now - 10800,
		LastExportAt: now,
	})
	c.measurementState.SetMetadata(8002, MeasurementMeta{
		TargetLocation: "us-east-1",
		TargetAddress:  "34.192.0.54",
		Sources: []SourceProbeMeta{
			{LocationCode: "us-west-2", ProbeID: 1000901, LastResponseAt: 0},
		},
		CreatedAt: now - 10800,
	})

	err := c.configureMeasurements(t.Context(), cloudFleetLocations(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	require.Equal(t, []int{1000901}, c.measurementState.GetUnresponsiveProbes(),
		"the one source that has delivered nothing to a measurement that is exporting must be blamed")
	require.False(t, c.measurementState.IsProbeUnresponsive(1000731),
		"a source that is delivering must not be blamed")
}

func TestInternetLatency_RIPEAtlas_CloudBlame_FleetThatStoppedExportingIsNotTornDown(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var stoppedMeasurements []int
	var mu sync.Mutex

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, tag string) ([]Measurement, error) {
			return cloudFleetMeasurements(), nil
		},
		StopMeasurementFunc: func(ctx context.Context, measurementID int) error {
			mu.Lock()
			stoppedMeasurements = append(stoppedMeasurements, measurementID)
			mu.Unlock()
			return nil
		},
	}

	stateDir := t.TempDir()
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudFleetNodes())

	// The measurements were recreated three hours ago and delivered for a while, then every
	// export stopped: the sources enlisted at recreation still read zero.
	now := time.Now().Unix()
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, CloudTimestampFileName))
	c.measurementState.SetMetadata(8001, MeasurementMeta{
		TargetLocation: "eu-west-1",
		TargetAddress:  "3.248.0.0",
		Sources: []SourceProbeMeta{
			{LocationCode: "us-east-1", ProbeID: 1000731, LastResponseAt: 0},
			{LocationCode: "us-west-2", ProbeID: 1000901, LastResponseAt: 0},
		},
		CreatedAt:    now - 10800,
		LastExportAt: now - 7200,
	})
	c.measurementState.SetMetadata(8002, MeasurementMeta{
		TargetLocation: "us-east-1",
		TargetAddress:  "34.192.0.54",
		Sources: []SourceProbeMeta{
			{LocationCode: "us-west-2", ProbeID: 1000901, LastResponseAt: 0},
		},
		CreatedAt:    now - 10800,
		LastExportAt: now - 7200,
	})

	err := c.configureMeasurements(t.Context(), cloudFleetLocations(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	require.Empty(t, c.measurementState.GetUnresponsiveProbes(),
		"a measurement that has stopped exporting names no source probe as the cause")

	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, stoppedMeasurements, "a fleet that has stopped exporting must not be torn down")
}
