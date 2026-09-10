package ripeatlas

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/stretchr/testify/require"
)

// cloudTestNodes returns two AWS regions; eu-west-1 sorts first, so it is the measurement target.
func cloudTestNodes() []CloudNode {
	return []CloudNode{
		{
			Code:          "eu-west-1",
			Cloud:         "aws",
			Latitude:      53.3498,
			Longitude:     -6.2603,
			AtlasProbeIDs: []int{1000441},
			PingTarget:    "3.248.0.0",
		},
		{
			Code:          "us-east-1",
			Cloud:         "aws",
			Latitude:      39.0438,
			Longitude:     -77.4874,
			AtlasProbeIDs: []int{1000731, 1000732},
			PingTarget:    "34.192.0.54",
		},
	}
}

func newCloudTestCollector(t *testing.T, log *slog.Logger, client clientInterface, env string, nodes []CloudNode) *Collector {
	t.Helper()

	c := NewCloudCollector(log, nil, env, nodes)
	c.client = client
	return c
}

// exchangeTestLocations returns three exchanges with two probes each, at distinct distances so
// probe selection is deterministic.
func exchangeTestLocations() []LocationProbeMatch {
	return []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{LocationCode: "nyc", Latitude: 40.7128, Longitude: -74.0060},
			NearbyProbes: []Probe{
				{ID: 100, Address: "1.1.1.1", Latitude: 40.7128, Longitude: -74.0060},
				{ID: 101, Address: "1.1.1.2", Latitude: 41.0000, Longitude: -74.0060},
			},
			ProbeCount: 2,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "lon", Latitude: 51.5074, Longitude: -0.1278},
			NearbyProbes: []Probe{
				{ID: 200, Address: "2.2.2.1", Latitude: 51.5074, Longitude: -0.1278},
				{ID: 201, Address: "2.2.2.2", Latitude: 52.0000, Longitude: -0.1278},
			},
			ProbeCount: 2,
		},
		{
			LocationMatch: collector.LocationMatch{LocationCode: "ams", Latitude: 52.3676, Longitude: 4.9041},
			NearbyProbes: []Probe{
				{ID: 300, Address: "3.3.3.1", Latitude: 52.3676, Longitude: 4.9041},
				{ID: 301, Address: "3.3.3.2", Latitude: 53.0000, Longitude: 4.9041},
			},
			ProbeCount: 2,
		},
	}
}

func TestInternetLatency_RIPEAtlas_Cloud_DisabledByDefault(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := NewCollector(log, nil, "testnet", func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	})

	require.False(t, c.cloudMode, "NewCollector must not enable cloud mode")
	require.Empty(t, c.cloudNodes, "NewCollector must not carry node file entries")

	measurementState := NewMeasurementState(filepath.Join(t.TempDir(), TimestampFileName))
	wanted := c.generateWantedMeasurements(exchangeTestLocations(), 1, measurementState)

	require.Len(t, wanted, 2, "three locations give two measurements, one per unordered pair")

	require.Equal(t, "ams", wanted[0].TargetLocationCode)
	require.Equal(t, 300, wanted[0].TargetProbe.ID)
	require.Equal(t, "3.3.3.1", wanted[0].TargetProbe.Address)
	require.Len(t, wanted[0].SourceSpecs, 2)
	require.Equal(t, "lon", wanted[0].SourceSpecs[0].LocationCode)
	require.Equal(t, 200, wanted[0].SourceSpecs[0].Probe.ID)
	require.Equal(t, "nyc", wanted[0].SourceSpecs[1].LocationCode)
	require.Equal(t, 100, wanted[0].SourceSpecs[1].Probe.ID)

	require.Equal(t, "lon", wanted[1].TargetLocationCode)
	require.Equal(t, 200, wanted[1].TargetProbe.ID)
	require.Len(t, wanted[1].SourceSpecs, 1)
	require.Equal(t, "nyc", wanted[1].SourceSpecs[0].LocationCode)
	require.Equal(t, 100, wanted[1].SourceSpecs[0].Probe.ID)
}

func TestInternetLatency_RIPEAtlas_Cloud_ConstructorEnablesMode(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := newCloudTestCollector(t, log, &MockClient{}, "mainnet-beta", cloudTestNodes())

	require.True(t, c.cloudMode, "NewCloudCollector must enable cloud mode")
	require.Len(t, c.cloudNodes, 2)
	require.Equal(t, "3.248.0.0", c.cloudNodes["eu-west-1"].PingTarget)
	require.Equal(t, []int{1000731, 1000732}, c.cloudNodes["us-east-1"].AtlasProbeIDs)

	locations := c.getLocationsFunc(t.Context())
	require.Len(t, locations, 2)
	require.Equal(t, "eu-west-1", locations[0].LocationCode)
	require.Equal(t, 53.3498, locations[0].Latitude)
	require.Equal(t, "us-east-1", locations[1].LocationCode)
}
