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

func TestInternetLatency_RIPEAtlas_CloudIdentity_ModeNames(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	exchange := NewCollector(log, nil, "mainnet-beta", func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{}
	})
	cloud := newCloudTestCollector(t, log, &MockClient{}, "mainnet-beta", cloudTestNodes())

	require.Equal(t, TimestampFileName, exchange.timestampFileName())
	require.Equal(t, "mainnet-beta", exchange.measurementTag())
	require.Equal(t, "DoubleZero ", exchange.descriptionPrefix())

	require.Equal(t, CloudTimestampFileName, cloud.timestampFileName())
	require.Equal(t, "mainnet-beta-cloud", cloud.measurementTag())
	require.Equal(t, "DoubleZero Cloud ", cloud.descriptionPrefix())

	const exchangeDescription = "DoubleZero [mainnet-beta] to xams probe 6626"
	const cloudDescription = "DoubleZero Cloud [mainnet-beta] to eu-west-1 target 3.248.0.0"

	require.True(t, exchange.ownsDescription(exchangeDescription))
	require.False(t, exchange.ownsDescription(cloudDescription))
	require.True(t, cloud.ownsDescription(cloudDescription))
	require.False(t, cloud.ownsDescription(exchangeDescription))

	location, ok := exchange.targetLocationFromDescription(exchangeDescription)
	require.True(t, ok)
	require.Equal(t, "xams", location)

	location, ok = cloud.targetLocationFromDescription(cloudDescription)
	require.True(t, ok)
	require.Equal(t, "eu-west-1", location)
}

func TestInternetLatency_RIPEAtlas_CloudIdentity_IgnoresExchangeMeasurements(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	var requestedTags []string
	var createdMeasurements []MeasurementRequest
	var stoppedMeasurements []int
	var mu sync.Mutex

	exchangeMeasurement := Measurement{
		ID:          1001,
		Description: "DoubleZero [mainnet-beta] to xams probe 6626",
		Target:      "84.38.236.1",
		Status: struct {
			Name string `json:"name"`
			ID   int    `json:"id"`
		}{Name: "Ongoing"},
		Type: "ping",
	}

	mockClient := &MockClient{
		GetAllMeasurementsFunc: func(ctx context.Context, tag string) ([]Measurement, error) {
			mu.Lock()
			requestedTags = append(requestedTags, tag)
			mu.Unlock()
			return []Measurement{exchangeMeasurement}, nil
		},
		CreateMeasurementFunc: func(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
			mu.Lock()
			createdMeasurements = append(createdMeasurements, request)
			measurementID := 7000 + len(createdMeasurements)
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
	c := newCloudTestCollector(t, log, mockClient, "mainnet-beta", cloudTestNodes())

	err := c.configureMeasurements(t.Context(), cloudTestLocationMatches(), false, 1, stateDir, 10*time.Minute)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.NotEmpty(t, requestedTags)
	for _, tag := range requestedTags {
		require.Equal(t, "mainnet-beta-cloud", tag, "cloud mode must filter on its own RIPE Atlas tag")
	}

	require.Empty(t, stoppedMeasurements, "an exchange measurement must never be stopped by cloud mode")

	require.Len(t, createdMeasurements, 1)
	require.Equal(t, "DoubleZero Cloud [mainnet-beta] to eu-west-1 target 3.248.0.0",
		createdMeasurements[0].Definitions[0].Description)
	require.ElementsMatch(t, []string{"mainnet-beta-cloud", "doublezero-cloud"},
		createdMeasurements[0].Definitions[0].Tags)

	require.FileExists(t, filepath.Join(stateDir, CloudTimestampFileName))
	require.NoFileExists(t, filepath.Join(stateDir, TimestampFileName))
}
