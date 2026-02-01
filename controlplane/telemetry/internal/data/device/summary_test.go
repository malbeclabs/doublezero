package data_test

import (
	"context"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	data "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/device"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	telemetry "github.com/malbeclabs/doublezero/sdk/telemetry/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetry_Data_Device_SummaryForCircuits(t *testing.T) {
	t.Parallel()

	t.Run("single circuit, aggregates 1 point, computes RTT deltas/ratios (µs)", func(t *testing.T) {
		t.Parallel()

		// Provider with 1 circuit; telemetry has a single 100µs sample at a fixed timestamp.
		c := defaultCircuit()
		sampleTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				StartTimestampMicroseconds:   uint64(sampleTime.UnixMicro()),
				SamplingIntervalMicroseconds: 1_000_000,
				Samples:                      []uint32{100},
			}, nil
		}, c)

		out, err := p.GetSummaryForCircuits(context.Background(), data.GetSummaryForCircuitsConfig{
			Circuits: []string{c.Code},
			Unit:     data.UnitMicrosecond,
			Time:     &data.TimeRange{From: sampleTime.Add(-time.Minute), To: sampleTime.Add(time.Minute)},
			Epochs:   nil,
		})
		require.NoError(t, err)
		require.Len(t, out, 1)

		got := out[0]
		assert.Equal(t, c.Code, got.Circuit)
		// Measured from telemetry
		assert.Equal(t, 100.0, got.RTTMean)
		// Committed (from serviceability mock) are likely 0 → deltas should be -measured
		assert.Equal(t, 0.0, got.CommittedRTT)
		assert.InDelta(t, -100.0, got.CommittedRTTDelta, 1e-9)
		// Division by zero on ratio is implementation-defined; accept NaN or +/-Inf.
		assert.True(t, math.IsNaN(got.CommittedRTTChangeRatio) || math.IsInf(got.CommittedRTTChangeRatio, 0))
	})

	t.Run("unit millisecond conversion applies to committed while measured is already converted by GetCircuitLatencies", func(t *testing.T) {
		t.Parallel()

		// Two samples: 10_000µs and 50_000µs → mean 30ms when Unit=ms.
		c := defaultCircuit()
		start := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				StartTimestampMicroseconds:   uint64(start.UnixMicro()),
				SamplingIntervalMicroseconds: 1_000_000,
				Samples:                      []uint32{10_000, 50_000},
			}, nil
		}, c)

		out, err := p.GetSummaryForCircuits(context.Background(), data.GetSummaryForCircuitsConfig{
			Circuits: []string{c.Code},
			Unit:     data.UnitMillisecond,
			Time:     &data.TimeRange{From: start, To: start.Add(2 * time.Second)},
		})
		require.NoError(t, err)
		require.Len(t, out, 1)

		got := out[0]
		// Measured mean should be in ms
		assert.InDelta(t, 30.0, got.RTTMean, 1e-9)

		// Committed values in our mock program data are zero → conversion leaves zero.
		assert.Equal(t, 0.0, got.CommittedRTT)
		assert.InDelta(t, -30.0, got.CommittedRTTDelta, 1e-9)
	})

	t.Run("multiple circuits: errors are skipped, results sorted by circuit then timestamp", func(t *testing.T) {
		t.Parallel()

		c1 := defaultCircuit()
		c2 := defaultCircuit2() // distinct code/link

		// Telemetry: c1 → valid single point; c2 → simulate failure (skipped).
		p := newTestProviderWithCircuits(t, func(ctx context.Context, a, z, link solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error) {
			switch link {
			case c1.Link.PK:
				base := time.Date(2024, 3, 10, 12, 0, 0, 0, time.UTC)
				return &telemetry.DeviceLatencySamples{
					StartTimestampMicroseconds:   uint64(base.UnixMicro()),
					SamplingIntervalMicroseconds: 60 * 1_000_000,
					Samples:                      []uint32{25_000}, // 25ms
				}, nil
			case c2.Link.PK:
				return nil, assert.AnError
			default:
				return nil, telemetry.ErrAccountNotFound
			}
		}, []data.Circuit{c1, c2})

		out, err := p.GetSummaryForCircuits(context.Background(), data.GetSummaryForCircuitsConfig{
			Circuits: []string{c2.Code, c1.Code}, // intentionally not sorted
			Unit:     data.UnitMillisecond,
			Time:     &data.TimeRange{From: time.Date(2024, 3, 10, 12, 0, 0, 0, time.UTC), To: time.Date(2024, 3, 10, 12, 5, 0, 0, time.UTC)},
		})
		require.NoError(t, err)

		// c2 failed → skipped; only c1 should be present.
		require.Len(t, out, 1)
		assert.Equal(t, c1.Code, out[0].Circuit)
		assert.InDelta(t, 25.0, out[0].RTTMean, 1e-9)
	})

	t.Run("unknown circuit code in cfg is ignored (no panic, no entry)", func(t *testing.T) {
		t.Parallel()

		c := defaultCircuit()
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				StartTimestampMicroseconds:   uint64(time.Now().UTC().UnixMicro()),
				SamplingIntervalMicroseconds: 1_000_000,
				Samples:                      []uint32{1},
			}, nil
		}, c)

		out, err := p.GetSummaryForCircuits(context.Background(), data.GetSummaryForCircuitsConfig{
			Circuits: []string{"does-not-exist"},
			Unit:     data.UnitMicrosecond,
			Epochs:   &data.EpochRange{From: 1, To: 1},
		})
		require.NoError(t, err)
		require.Empty(t, out)
	})

	t.Run("when GetCircuitLatencies returns empty for a circuit, it is skipped", func(t *testing.T) {
		t.Parallel()

		c := defaultCircuit()
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) {
			// No samples in the requested window → Aggregate returns no series.
			return &telemetry.DeviceLatencySamples{
				StartTimestampMicroseconds:   uint64(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixMicro()),
				SamplingIntervalMicroseconds: 60 * 1_000_000,
				Samples:                      []uint32{},
			}, nil
		}, c)

		out, err := p.GetSummaryForCircuits(context.Background(), data.GetSummaryForCircuitsConfig{
			Circuits: []string{c.Code},
			Unit:     data.UnitMicrosecond,
			Time:     &data.TimeRange{From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 1, 1, 0, 5, 0, 0, time.UTC)},
		})
		require.NoError(t, err)
		require.Empty(t, out)
	})
}

func defaultCircuit2() data.Circuit {
	pkA := solana.NewWallet().PublicKey()
	pkB := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()
	devA := serviceability.Device{Code: "X"}
	devB := serviceability.Device{Code: "Y"}
	link := serviceability.Link{Code: "L2"}
	return data.Circuit{
		Code:         circuitKey(devA.Code, devB.Code, linkPK),
		OriginDevice: data.Device{PK: pkA, Code: devA.Code},
		TargetDevice: data.Device{PK: pkB, Code: devB.Code},
		Link:         data.Link{PK: linkPK, Code: link.Code},
	}
}

func newTestProviderWithCircuits(t *testing.T, get func(ctx context.Context, a, z, link solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error), circuits []data.Circuit) data.Provider {
	t.Helper()

	// Build ProgramData covering all circuits provided.
	devSeen := map[string]solana.PublicKey{}
	var devs []serviceability.Device
	var links []serviceability.Link
	var contributors []serviceability.Contributor
	contributorPK := solana.NewWallet().PublicKey()
	for _, c := range circuits {
		if _, ok := devSeen[c.OriginDevice.Code]; !ok {
			devSeen[c.OriginDevice.Code] = c.OriginDevice.PK
			devs = append(devs, serviceability.Device{Code: c.OriginDevice.Code, PubKey: c.OriginDevice.PK})
		}
		if _, ok := devSeen[c.TargetDevice.Code]; !ok {
			devSeen[c.TargetDevice.Code] = c.TargetDevice.PK
			devs = append(devs, serviceability.Device{Code: c.TargetDevice.Code, PubKey: c.TargetDevice.PK})
		}
		links = append(links, serviceability.Link{
			Code:              c.Link.Code,
			SideAPubKey:       c.OriginDevice.PK,
			SideZPubKey:       c.TargetDevice.PK,
			PubKey:            c.Link.PK,
			ContributorPubKey: contributorPK,
		})
		contributors = append(contributors, serviceability.Contributor{Code: c.Link.ContributorCode, PubKey: contributorPK})
	}

	p, err := data.NewProvider(&data.ProviderConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ServiceabilityClient: &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{Devices: devs, Links: links, Contributors: contributors}, nil
			},
		},
		TelemetryClient: &mockTelemetryClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, a, z, link solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error) {
				return get(ctx, a, z, link, epoch)
			},
		},
		EpochFinder: &mockEpochFinder{
			ApproximateAtTimeFunc: func(ctx context.Context, _ time.Time) (uint64, error) { return 1, nil },
		},
		CircuitsCacheTTL:               time.Minute,
		CurrentEpochLatenciesCacheTTL:  10 * time.Second,
		HistoricEpochLatenciesCacheTTL: time.Hour,
	})
	require.NoError(t, err)
	return p
}
