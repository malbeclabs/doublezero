package data_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	data "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/internet"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetry_Data_Internet_Provider_GetCircuitLatencies(t *testing.T) {
	t.Parallel()

	t.Run("epoch cache hit", func(t *testing.T) {
		t.Parallel()

		var called int
		provider := newTestProvider(t, func(epoch uint64) (*telemetry.InternetLatencySamples, error) {
			called++
			if called > 1 {
				require.Fail(t, "should not call GetInternetLatencySamples more than once")
			}
			return &telemetry.InternetLatencySamples{
				InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
					StartTimestampMicroseconds:   1_600_000_000_000_000,
					SamplingIntervalMicroseconds: 1_000_000, // 1ms
				},
				Samples: []uint32{10, 20},
			}, nil
		}, defaultCircuit())

		ctx := context.Background()
		epoch := uint64(1)

		first, err := provider.GetCircuitLatenciesForEpoch(ctx, "A → B", epoch, "")
		require.NoError(t, err)
		require.Len(t, first, 2)

		_, err = provider.GetCircuitLatenciesForEpoch(ctx, "B → A", epoch, "")
		require.ErrorContains(t, err, "circuit not found")
	})

	t.Run("epoch account not found caches empty", func(t *testing.T) {
		t.Parallel()

		provider := newTestProvider(t, func(epoch uint64) (*telemetry.InternetLatencySamples, error) {
			return nil, telemetry.ErrAccountNotFound
		}, defaultCircuit())

		epoch := uint64(1)
		latencies, err := provider.GetCircuitLatenciesForEpoch(context.Background(), "A → B", epoch, "")
		assert.ErrorIs(t, err, telemetry.ErrAccountNotFound)
		assert.Empty(t, latencies)
	})

	t.Run("GetCircuitLatencies filters by time", func(t *testing.T) {
		t.Parallel()

		sampleTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
		sampleMicros := uint64(sampleTime.UnixMicro())

		provider := newTestProvider(t, func(epoch uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{
				InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
					StartTimestampMicroseconds:   sampleMicros,
					SamplingIntervalMicroseconds: 1_000_000, // 1ms
				},
				Samples: []uint32{42},
			}, nil
		}, defaultCircuit())

		from := time.Date(2023, 1, 1, 11, 55, 0, 0, time.UTC)
		to := time.Date(2023, 1, 1, 12, 5, 0, 0, time.UTC)
		latencies, err := provider.GetCircuitLatenciesForTimeRange(context.Background(), "A → B", from, to, "")
		require.NoError(t, err)
		require.Len(t, latencies, 1)
		assert.Equal(t, uint32(42), latencies[0].RTT)
	})

	t.Run("GetCircuitLatenciesDownsampled with maxPoints=1 returns aggregate", func(t *testing.T) {
		from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		tsMicros := uint64(from.UnixMicro())

		provider := newTestProvider(t, func(epoch uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{
				InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
					StartTimestampMicroseconds:   tsMicros,
					SamplingIntervalMicroseconds: 1_000_000, // 1s intervals
				},
				Samples: []uint32{10, 20, 30},
			}, nil
		}, defaultCircuit())

		to := from.Add(3 * time.Second)

		stats, err := provider.GetCircuitLatenciesDownsampled(context.Background(), "A → B", from, to, 1, data.UnitMicrosecond, "")
		require.NoError(t, err)
		require.Len(t, stats, 1)
		assert.Equal(t, "A → B", stats[0].Circuit)
		assert.InDelta(t, 20.0, stats[0].RTTMean, 0.1)
		assert.Equal(t, float64(10), stats[0].RTTMin)
		assert.Equal(t, float64(30), stats[0].RTTMax)
	})

	t.Run("Downsampled returns multiple buckets when maxPoints > 1", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
		tsMicros := uint64(now.UnixMicro())

		provider := newTestProvider(t, func(epoch uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{
				InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
					StartTimestampMicroseconds:   tsMicros,
					SamplingIntervalMicroseconds: 60 * 1_000_000, // 1-minute spacing
				},
				Samples: []uint32{10, 20, 30, 40, 50},
			}, nil
		}, defaultCircuit())

		from := now
		to := now.Add(5 * time.Minute) // ensure at least 5 minutes span
		stats, err := provider.GetCircuitLatenciesDownsampled(context.Background(), "A → B", from, to, 2, data.UnitMicrosecond, "")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(stats), 2, "expected at least 2 downsampled buckets")
	})

	t.Run("Downsampled returns empty when no data in range", func(t *testing.T) {
		t.Parallel()

		provider := newTestProvider(t, func(epoch uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{
				InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
					StartTimestampMicroseconds:   0,
					SamplingIntervalMicroseconds: 1_000_000,
				},
				Samples: []uint32{},
			}, nil
		}, defaultCircuit())

		now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
		stats, err := provider.GetCircuitLatenciesDownsampled(context.Background(), "A → B", now, now.Add(1*time.Minute), 1, data.UnitMicrosecond, "")
		require.NoError(t, err)
		assert.Len(t, stats, 0)
	})

	t.Run("GetCircuitLatenciesDownsampled with invalid unit", func(t *testing.T) {
		t.Parallel()

		provider := newTestProvider(t, func(epoch uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{}, nil
		}, defaultCircuit())

		now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
		stats, err := provider.GetCircuitLatenciesDownsampled(t.Context(), "A → B", now, now.Add(1*time.Second), 1, "invalid", "")
		require.Error(t, err)
		assert.Empty(t, stats)
	})

	t.Run("GetCircuitLatenciesDownsampled with unit=ms", func(t *testing.T) {
		t.Parallel()

		from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		tsMicros := uint64(from.UnixMicro())

		provider := newTestProvider(t, func(epoch uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{
				InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
					StartTimestampMicroseconds:   tsMicros,
					SamplingIntervalMicroseconds: 1_000_000, // 1s intervals
				},
				Samples: []uint32{10_000, 20_000, 30_000},
			}, nil
		}, defaultCircuit())

		to := from.Add(3 * time.Second)

		stats, err := provider.GetCircuitLatenciesDownsampled(t.Context(), "A → B", from, to, 1, data.UnitMillisecond, "")
		require.NoError(t, err)
		require.Len(t, stats, 1)
		assert.Equal(t, "A → B", stats[0].Circuit)
		assert.InDelta(t, 20.0, stats[0].RTTMean, 0.1)
		assert.Equal(t, float64(10), stats[0].RTTMin)
		assert.Equal(t, float64(30), stats[0].RTTMax)
	})
}

func defaultCircuit() data.Circuit {
	locA := serviceability.Location{Code: "A", PubKey: solana.NewWallet().PublicKey()}
	locB := serviceability.Location{Code: "B", PubKey: solana.NewWallet().PublicKey()}

	return data.Circuit{
		Code:           circuitKey(locA.Code, locB.Code),
		OriginLocation: data.Location{PK: locA.PubKey, Code: locA.Code, Name: locA.Name, Country: locA.Country, Latitude: locA.Lat, Longitude: locA.Lng},
		TargetLocation: data.Location{PK: locB.PubKey, Code: locB.Code, Name: locB.Name, Country: locB.Country, Latitude: locB.Lat, Longitude: locB.Lng},
	}
}

func circuitKey(origin, target string) string {
	return fmt.Sprintf("%s → %s", origin, target)
}

func newTestProvider(
	t *testing.T,
	getFunc func(epoch uint64) (*telemetry.InternetLatencySamples, error),
	circuit data.Circuit,
) data.Provider {
	provider, err := data.NewProvider(&data.ProviderConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ServiceabilityClient: &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Locations: []serviceability.Location{
						{Code: circuit.OriginLocation.Code, PubKey: circuit.OriginLocation.PK},
						{Code: circuit.TargetLocation.Code, PubKey: circuit.TargetLocation.PK},
					},
				}, nil
			},
		},
		TelemetryClient: &mockTelemetryClient{
			GetInternetLatencySamplesFunc: func(ctx context.Context, _ string, _, _, _ solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error) {
				return getFunc(epoch)
			},
		},
		EpochFinder: &mockEpochFinder{
			ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return 1, nil
			},
		},
		AgentPK:                        solana.NewWallet().PublicKey(),
		CircuitsCacheTTL:               time.Minute,
		CurrentEpochLatenciesCacheTTL:  10 * time.Second,
		HistoricEpochLatenciesCacheTTL: time.Hour,
	})
	require.NoError(t, err)
	return provider
}
