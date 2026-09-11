package ripeatlas

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/stretchr/testify/require"
)

// cloudProbeTestNodes gives us-east-1 three ordered candidates. eu-west-1 sorts first, so it is
// the measurement target and us-east-1 is the only source.
func cloudProbeTestNodes() []CloudNode {
	nodes := cloudTestNodes()
	nodes[1].AtlasProbeIDs = []int{1000731, 1000732, 1000733}
	return nodes
}

// cloudDelistedProbeNodes drops 1000731 from us-east-1, so an earlier cycle's incumbent is no
// longer on the list the node file names.
func cloudDelistedProbeNodes() []CloudNode {
	nodes := cloudProbeTestNodes()
	nodes[1].AtlasProbeIDs = []int{1000732, 1000733}
	return nodes
}

// cloudProbeLocations returns the two regions with no probes attached, as the measurement cycle
// hands them to selection.
func cloudProbeLocations() []LocationProbeMatch {
	return []LocationProbeMatch{
		{LocationMatch: collector.LocationMatch{LocationCode: "eu-west-1", Latitude: 53.3498, Longitude: -6.2603}},
		{LocationMatch: collector.LocationMatch{LocationCode: "us-east-1", Latitude: 39.0438, Longitude: -77.4874}},
	}
}

func cloudProbeTestState(t *testing.T) *MeasurementState {
	t.Helper()

	return NewMeasurementState(filepath.Join(t.TempDir(), CloudTimestampFileName))
}

// enlist records the source probe an earlier cycle chose, which is where stickiness reads from.
func enlist(state *MeasurementState, measurementID int, locationCode string, probeID int) {
	state.SetMetadata(measurementID, MeasurementMeta{
		TargetLocation: "eu-west-1",
		TargetAddress:  "3.248.0.0",
		Sources:        []SourceProbeMeta{{LocationCode: locationCode, ProbeID: probeID, LastResponseAt: time.Now().Unix()}},
		CreatedAt:      time.Now().Unix() - 60,
		LastExportAt:   time.Now().Unix(),
	})
}

func selectedProbes(matches []LocationProbeMatch) map[string]int {
	selected := map[string]int{}
	for _, match := range matches {
		if len(match.NearbyProbes) > 0 {
			selected[match.LocationCode] = match.NearbyProbes[0].ID
		}
	}
	return selected
}

func TestInternetLatency_RIPEAtlas_CloudProbes_FirstLiveCandidateWins(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := newCloudTestCollector(t, log, &MockClient{}, "mainnet-beta", cloudProbeTestNodes())
	live := map[int]bool{1000441: true, 1000731: true, 1000732: true, 1000733: true}

	matches := c.selectCloudProbes(cloudProbeLocations(), live, cloudProbeTestState(t))

	require.Equal(t, map[string]int{"eu-west-1": 1000441, "us-east-1": 1000731}, selectedProbes(matches))
	for _, match := range matches {
		require.Len(t, match.NearbyProbes, 1, "a region measures from one probe, however many are live")
		require.Equal(t, 1, match.ProbeCount)
	}
}

func TestInternetLatency_RIPEAtlas_CloudProbes_DeadCandidateFallsThrough(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := newCloudTestCollector(t, log, &MockClient{}, "mainnet-beta", cloudProbeTestNodes())
	live := map[int]bool{1000441: true, 1000732: true, 1000733: true}

	matches := c.selectCloudProbes(cloudProbeLocations(), live, cloudProbeTestState(t))

	require.Equal(t, 1000732, selectedProbes(matches)["us-east-1"],
		"a disconnected first entry hands the seat to the next one in the list")
}

func TestInternetLatency_RIPEAtlas_CloudProbes_LiveIncumbentIsKept(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := newCloudTestCollector(t, log, &MockClient{}, "mainnet-beta", cloudProbeTestNodes())
	state := cloudProbeTestState(t)
	enlist(state, 8001, "us-east-1", 1000732)

	live := map[int]bool{1000441: true, 1000731: true, 1000732: true, 1000733: true}
	matches := c.selectCloudProbes(cloudProbeLocations(), live, state)

	require.Equal(t, 1000732, selectedProbes(matches)["us-east-1"],
		"an earlier-listed probe reconnecting must not reclaim the seat")
}

func TestInternetLatency_RIPEAtlas_CloudProbes_BlacklistedIncumbentMovesOn(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := newCloudTestCollector(t, log, &MockClient{}, "mainnet-beta", cloudProbeTestNodes())
	state := cloudProbeTestState(t)
	enlist(state, 8001, "us-east-1", 1000731)
	state.AddUnresponsiveProbe(1000731)

	live := map[int]bool{1000441: true, 1000731: true, 1000732: true, 1000733: true}
	matches := c.selectCloudProbes(cloudProbeLocations(), live, state)

	require.Equal(t, 1000732, selectedProbes(matches)["us-east-1"],
		"an unresponsive incumbent hands the seat on even while RIPE reports it Connected")
}

func TestInternetLatency_RIPEAtlas_CloudProbes_AllCandidatesDeadKeepsIncumbent(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := newCloudTestCollector(t, log, &MockClient{}, "mainnet-beta", cloudProbeTestNodes())
	state := cloudProbeTestState(t)
	enlist(state, 8001, "us-east-1", 1000732)

	matches := c.selectCloudProbes(cloudProbeLocations(), map[int]bool{}, state)

	require.Equal(t, 1000732, selectedProbes(matches)["us-east-1"],
		"a region with nothing live keeps its dead probe rather than dropping out")
	require.NotContains(t, selectedProbes(matches), "eu-west-1",
		"a region with nothing live and nothing enlisted contributes no probe")
}

func TestInternetLatency_RIPEAtlas_CloudProbes_NewestEnlistmentWins(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := newCloudTestCollector(t, log, &MockClient{}, "mainnet-beta", cloudProbeTestNodes())
	state := cloudProbeTestState(t)
	enlist(state, 7001, "us-east-1", 1000732)
	enlist(state, 9001, "us-east-1", 1000733)

	live := map[int]bool{1000441: true, 1000731: true, 1000732: true, 1000733: true}
	matches := c.selectCloudProbes(cloudProbeLocations(), live, state)

	require.Equal(t, 1000733, selectedProbes(matches)["us-east-1"],
		"two disagreeing enlistments resolve to the one the later measurement carries")
}

func TestInternetLatency_RIPEAtlas_CloudProbes_DelistedIncumbentLosesTheSeat(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := newCloudTestCollector(t, log, &MockClient{}, "mainnet-beta", cloudDelistedProbeNodes())
	state := cloudProbeTestState(t)
	enlist(state, 8001, "us-east-1", 1000731)

	matches := c.selectCloudProbes(cloudProbeLocations(), map[int]bool{}, state)

	require.NotContains(t, selectedProbes(matches), "us-east-1",
		"a probe the node file no longer lists must not keep the seat, even with nothing else live")
}

func TestInternetLatency_RIPEAtlas_CloudProbes_LivenessQueryCoversEveryConfiguredID(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var requested [][]int
	var mu sync.Mutex

	mockClient := &MockClient{
		GetConnectedProbeIDsFunc: func(ctx context.Context, probeIDs []int) (map[int]bool, error) {
			mu.Lock()
			requested = append(requested, probeIDs)
			mu.Unlock()
			return map[int]bool{1000441: true, 1000731: true}, nil
		},
	}

	stateDir := t.TempDir()
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudProbeTestNodes())

	err := c.RunRipeAtlasMeasurementCreation(t.Context(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, requested, 1, "liveness costs one call per measurement cycle")
	require.Equal(t, []int{1000441, 1000731, 1000732, 1000733}, requested[0])
}

func TestInternetLatency_RIPEAtlas_CloudProbes_NoDiscoveryNoRotation(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var createdMeasurements []MeasurementRequest
	var probesInRadiusCalls int
	var probesForLocationsCalls int
	var mu sync.Mutex

	mockClient := &MockClient{
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			measurementID := 5000 + len(createdMeasurements)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{measurementID}}, nil
		},
		GetProbesInRadiusFunc: func(ctx context.Context, latitude, longitude float64, radiusKm int, anchorsOnly bool) ([]Probe, error) {
			mu.Lock()
			probesInRadiusCalls++
			mu.Unlock()
			return []Probe{{ID: 9999, Address: "5.5.5.5", Latitude: latitude, Longitude: longitude}}, nil
		},
		GetProbesForLocationsFunc: func(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error) {
			mu.Lock()
			probesForLocationsCalls++
			mu.Unlock()
			return locations, nil
		},
	}

	stateDir := t.TempDir()
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudProbeTestNodes())

	c.measurementState = NewMeasurementState(filepath.Join(stateDir, CloudTimestampFileName))
	c.measurementState.AddUnresponsiveProbe(1000731)

	err := c.RunRipeAtlasMeasurementCreation(t.Context(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.Equal(t, 0, probesForLocationsCalls, "cloud mode must not run probe discovery")
	require.Equal(t, 0, probesInRadiusCalls, "cloud mode must not look for replacement probes")

	require.Len(t, createdMeasurements, 1)
	require.Equal(t, "3.248.0.0", createdMeasurements[0].Definitions[0].Target)

	probeIDs := []int{}
	for _, probe := range createdMeasurements[0].Probes {
		probeIDs = append(probeIDs, probe.Value)
	}
	require.Equal(t, []int{1000732}, probeIDs,
		"the region measures from one probe out of its own list, with no substitute")
}

func TestInternetLatency_RIPEAtlas_CloudProbes_DeadIncumbentDoesNotRecreateMeasurement(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var createdMeasurements []MeasurementRequest
	var stoppedMeasurements []int
	var mu sync.Mutex

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, tag string) ([]Measurement, error) {
			return []Measurement{cloudMeasurement()}, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{9001}}, nil
		},
		StopMeasurementFunc: func(ctx context.Context, measurementID int) error {
			mu.Lock()
			stoppedMeasurements = append(stoppedMeasurements, measurementID)
			mu.Unlock()
			return nil
		},
		GetConnectedProbeIDsFunc: func(ctx context.Context, probeIDs []int) (map[int]bool, error) {
			return map[int]bool{}, nil
		},
	}

	stateDir := t.TempDir()
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudProbeTestNodes())

	c.measurementState = NewMeasurementState(filepath.Join(stateDir, CloudTimestampFileName))
	enlist(c.measurementState, 8001, "us-east-1", 1000732)

	err := c.RunRipeAtlasMeasurementCreation(t.Context(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.Empty(t, stoppedMeasurements, "a region with nothing live must not recreate what it feeds")
	require.Empty(t, createdMeasurements)
}

func TestInternetLatency_RIPEAtlas_CloudProbes_LivenessErrorChangesNothing(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var createdMeasurements []MeasurementRequest
	var stoppedMeasurements []int
	var mu sync.Mutex

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, tag string) ([]Measurement, error) {
			return []Measurement{cloudMeasurement()}, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{9002}}, nil
		},
		StopMeasurementFunc: func(ctx context.Context, measurementID int) error {
			mu.Lock()
			stoppedMeasurements = append(stoppedMeasurements, measurementID)
			mu.Unlock()
			return nil
		},
		GetConnectedProbeIDsFunc: func(ctx context.Context, probeIDs []int) (map[int]bool, error) {
			return nil, errors.New("ripe atlas unavailable")
		},
	}

	stateDir := t.TempDir()
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudProbeTestNodes())

	c.measurementState = NewMeasurementState(filepath.Join(stateDir, CloudTimestampFileName))
	enlist(c.measurementState, 8001, "us-east-1", 1000732)

	err := c.RunRipeAtlasMeasurementCreation(t.Context(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.Empty(t, stoppedMeasurements, "a failed liveness check must not churn probes")
	require.Empty(t, createdMeasurements)
}

func TestInternetLatency_RIPEAtlas_CloudProbes_NewlyUnresponsiveProbeRollsOverSameCycle(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var createdMeasurements []MeasurementRequest
	var stoppedMeasurements []int
	var mu sync.Mutex

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, tag string) ([]Measurement, error) {
			return []Measurement{cloudMeasurement()}, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{9003}}, nil
		},
		StopMeasurementFunc: func(ctx context.Context, measurementID int) error {
			mu.Lock()
			stoppedMeasurements = append(stoppedMeasurements, measurementID)
			mu.Unlock()
			return nil
		},
	}

	stateDir := t.TempDir()
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudProbeTestNodes())

	// The enlisted probe has delivered nothing since creation, well past the grace period.
	c.measurementState = NewMeasurementState(filepath.Join(stateDir, CloudTimestampFileName))
	c.measurementState.SetMetadata(8001, MeasurementMeta{
		TargetLocation: "eu-west-1",
		TargetAddress:  "3.248.0.0",
		Sources:        []SourceProbeMeta{{LocationCode: "us-east-1", ProbeID: 1000731, LastResponseAt: 0}},
		CreatedAt:      time.Now().Unix() - 10800,
		LastExportAt:   time.Now().Unix(),
	})

	err := c.RunRipeAtlasMeasurementCreation(t.Context(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.Contains(t, stoppedMeasurements, 8001)
	require.Len(t, createdMeasurements, 1)

	probeIDs := []int{}
	for _, probe := range createdMeasurements[0].Probes {
		probeIDs = append(probeIDs, probe.Value)
	}
	require.Equal(t, []int{1000732}, probeIDs,
		"a probe marked unresponsive hands over within the same cycle")
}

func TestInternetLatency_RIPEAtlas_CloudProbes_DelistedProbeIsNeverEnlisted(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var createdMeasurements []MeasurementRequest
	var mu sync.Mutex

	mockClient := &MockClient{
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			mu.Unlock()
			return &MeasurementResponse{Measurements: []int{9004}}, nil
		},
		GetConnectedProbeIDsFunc: func(ctx context.Context, probeIDs []int) (map[int]bool, error) {
			return map[int]bool{}, nil
		},
	}

	stateDir := t.TempDir()
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudDelistedProbeNodes())

	c.measurementState = NewMeasurementState(filepath.Join(stateDir, CloudTimestampFileName))
	enlist(c.measurementState, 8001, "us-east-1", 1000731)

	err := c.RunRipeAtlasMeasurementCreation(t.Context(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	for _, request := range createdMeasurements {
		for _, probe := range request.Probes {
			require.NotEqual(t, 1000731, probe.Value,
				"a measurement must never be created from a probe the node file no longer lists")
		}
	}
	require.Empty(t, createdMeasurements,
		"a region whose remaining candidates are all dead contributes nothing this cycle")
}
