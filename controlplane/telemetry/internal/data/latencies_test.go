package data_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data"
	datapkg "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/data"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetry_Data_Provider_GetCircuitLatencies(t *testing.T) {
	t.Parallel()

	t.Run("epoch cache hit", func(t *testing.T) {
		t.Parallel()

		var called int
		provider := newTestProvider(t, func(epoch uint64) (*telemetry.DeviceLatencySamples, error) {
			called++
			if called > 1 {
				require.Fail(t, "should not call GetDeviceLatencySamples more than once")
			}
			return &telemetry.DeviceLatencySamples{
				DeviceLatencySamplesHeader: telemetry.DeviceLatencySamplesHeader{
					StartTimestampMicroseconds:   1_600_000_000_000_000,
					SamplingIntervalMicroseconds: 1_000_000, // 1ms
				},
				Samples: []uint32{10, 20},
			}, nil
		}, defaultCircuit())

		ctx := context.Background()
		epoch := datapkg.DeriveEpoch(time.Now())

		first, err := provider.GetCircuitLatenciesForEpoch(ctx, "A → B (L1)", epoch)
		require.NoError(t, err)
		require.Len(t, first, 2)

		second, err := provider.GetCircuitLatenciesForEpoch(ctx, "A → B (L1)", epoch)
		require.NoError(t, err)
		assert.Equal(t, first, second)
	})

	t.Run("epoch account not found caches empty", func(t *testing.T) {
		t.Parallel()

		provider := newTestProvider(t, func(epoch uint64) (*telemetry.DeviceLatencySamples, error) {
			return nil, telemetry.ErrAccountNotFound
		}, defaultCircuit())

		epoch := datapkg.DeriveEpoch(time.Now())
		latencies, err := provider.GetCircuitLatenciesForEpoch(context.Background(), "A → B (L1)", epoch)
		assert.ErrorIs(t, err, telemetry.ErrAccountNotFound)
		assert.Empty(t, latencies)
	})

	t.Run("GetCircuitLatencies filters by time", func(t *testing.T) {
		t.Parallel()

		now := time.Now()
		sampleTime := now.Add(-5 * time.Minute).UTC()
		sampleMicros := uint64(sampleTime.UnixMicro())

		provider := newTestProvider(t, func(epoch uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				DeviceLatencySamplesHeader: telemetry.DeviceLatencySamplesHeader{
					StartTimestampMicroseconds:   sampleMicros,
					SamplingIntervalMicroseconds: 1_000_000, // 1ms
				},
				Samples: []uint32{42},
			}, nil
		}, defaultCircuit())

		from := now.Add(-10 * time.Minute)
		to := now
		latencies, err := provider.GetCircuitLatencies(context.Background(), "A → B (L1)", from, to)
		require.NoError(t, err)
		require.Len(t, latencies, 1)
		assert.Equal(t, uint32(42), latencies[0].RTT)
	})

	t.Run("GetCircuitLatenciesDownsampled with maxPoints=1 returns aggregate", func(t *testing.T) {
		from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		tsMicros := uint64(from.UnixMicro())

		provider := newTestProvider(t, func(epoch uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				DeviceLatencySamplesHeader: telemetry.DeviceLatencySamplesHeader{
					StartTimestampMicroseconds:   tsMicros,
					SamplingIntervalMicroseconds: 1_000_000, // 1s intervals
				},
				Samples: []uint32{10, 20, 30},
			}, nil
		}, defaultCircuit())

		to := from.Add(3 * time.Second)

		stats, err := provider.GetCircuitLatenciesDownsampled(context.Background(), "A → B (L1)", from, to, 1)
		require.NoError(t, err)
		require.Len(t, stats, 1)
		assert.Equal(t, "A → B (L1)", stats[0].Circuit)
		assert.InDelta(t, 20.0, stats[0].RTTMean, 0.1)
		assert.Equal(t, float64(10), stats[0].RTTMin)
		assert.Equal(t, float64(30), stats[0].RTTMax)
	})

	t.Run("Downsampled returns multiple buckets when maxPoints > 1", func(t *testing.T) {
		t.Parallel()

		now := time.Now().UTC()
		tsMicros := uint64(now.UnixMicro())

		provider := newTestProvider(t, func(epoch uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				DeviceLatencySamplesHeader: telemetry.DeviceLatencySamplesHeader{
					StartTimestampMicroseconds:   tsMicros,
					SamplingIntervalMicroseconds: 60 * 1_000_000, // 1-minute spacing
				},
				Samples: []uint32{10, 20, 30, 40, 50},
			}, nil
		}, defaultCircuit())

		from := now
		to := now.Add(5 * time.Minute) // ensure at least 5 minutes span
		stats, err := provider.GetCircuitLatenciesDownsampled(context.Background(), "A → B (L1)", from, to, 2)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(stats), 2, "expected at least 2 downsampled buckets")
	})

	t.Run("Downsampled returns empty when no data in range", func(t *testing.T) {
		t.Parallel()

		provider := newTestProvider(t, func(epoch uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				DeviceLatencySamplesHeader: telemetry.DeviceLatencySamplesHeader{
					StartTimestampMicroseconds:   0,
					SamplingIntervalMicroseconds: 1_000_000,
				},
				Samples: []uint32{},
			}, nil
		}, defaultCircuit())

		now := time.Now()
		stats, err := provider.GetCircuitLatenciesDownsampled(context.Background(), "A → B (L1)", now, now.Add(1*time.Minute), 1)
		require.NoError(t, err)
		assert.Len(t, stats, 0)
	})
}

func defaultCircuit() []data.Circuit {
	pkA := solana.NewWallet().PublicKey()
	pkB := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()

	devA := serviceability.Device{Code: "A", PubKey: toPubKeyBytes(pkA)}
	devB := serviceability.Device{Code: "B", PubKey: toPubKeyBytes(pkB)}
	link := serviceability.Link{Code: "L1", SideAPubKey: toPubKeyBytes(pkA), SideZPubKey: toPubKeyBytes(pkB), PubKey: toPubKeyBytes(linkPK)}

	return []data.Circuit{
		{
			Code:         circuitKey(devA.Code, devB.Code, link.Code), // <== use actual keying logic
			OriginDevice: devA,
			TargetDevice: devB,
			Link:         link,
		},
	}
}

func circuitKey(origin, target, link string) string {
	return fmt.Sprintf("%s → %s (%s)", origin, target, link)
}

func newTestProvider(
	t *testing.T,
	getFunc func(epoch uint64) (*telemetry.DeviceLatencySamples, error),
	circuits []data.Circuit,
) data.Provider {
	provider, err := data.NewProvider(&data.ProviderConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ServiceabilityClient: &mockServiceabilityClient{
			LoadFunc: func(ctx context.Context) error { return nil },
			GetDevicesFunc: func() []serviceability.Device {
				dev := circuits[0].OriginDevice
				return []serviceability.Device{dev, circuits[0].TargetDevice}
			},
			GetLinksFunc: func() []serviceability.Link {
				return []serviceability.Link{circuits[0].Link}
			},
		},
		TelemetryClient: &mockTelemetryClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error) {
				return getFunc(epoch)
			},
		},
		CircuitsCacheTTL:               time.Minute,
		CurrentEpochLatenciesCacheTTL:  10 * time.Second,
		HistoricEpochLatenciesCacheTTL: time.Hour,
	})
	require.NoError(t, err)
	return provider
}
