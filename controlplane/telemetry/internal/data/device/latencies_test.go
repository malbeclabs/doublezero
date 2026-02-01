package data_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	data "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/device"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	telemetry "github.com/malbeclabs/doublezero/sdk/telemetry/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetry_Data_Device_Latencies(t *testing.T) {
	t.Parallel()

	t.Run("epoch cache hit via public API", func(t *testing.T) {
		t.Parallel()
		c := defaultCircuit()
		var calls int32
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) {
			atomic.AddInt32(&calls, 1)
			return &telemetry.DeviceLatencySamples{
				StartTimestampMicroseconds:   1_600_000_000_000_000,
				SamplingIntervalMicroseconds: 1_000_000,
				Samples:                      []uint32{10, 20},
			}, nil
		}, c)

		cfg := data.GetCircuitLatenciesConfig{
			Circuit:   c.Code,
			Unit:      data.UnitMicrosecond,
			Epochs:    &data.EpochRange{From: 1, To: 1},
			MaxPoints: 2,
		}

		stats1, err := p.GetCircuitLatencies(context.Background(), cfg)
		require.NoError(t, err)
		require.NotEmpty(t, stats1)

		stats2, err := p.GetCircuitLatencies(context.Background(), cfg)
		require.NoError(t, err)
		require.NotEmpty(t, stats2)

		assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "second call should hit cache, not the telemetry client")
	})

	t.Run("epoch account not found is skipped and returns empty (and short-caches internally)", func(t *testing.T) {
		t.Parallel()
		c := defaultCircuit()
		var calls int32
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) {
			atomic.AddInt32(&calls, 1)
			return nil, telemetry.ErrAccountNotFound
		}, c)

		cfg := data.GetCircuitLatenciesConfig{
			Circuit:   c.Code,
			Unit:      data.UnitMicrosecond,
			Epochs:    &data.EpochRange{From: 1, To: 1},
			MaxPoints: 1,
		}

		stats, err := p.GetCircuitLatencies(context.Background(), cfg)
		require.NoError(t, err)
		require.Empty(t, stats)

		stats2, err := p.GetCircuitLatencies(context.Background(), cfg)
		require.NoError(t, err)
		require.Empty(t, stats2)

		assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "second call should hit empty-cache for that epoch+provider")
	})
	t.Run("time range aggregates single point", func(t *testing.T) {
		t.Parallel()
		c := defaultCircuit()
		sampleTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				StartTimestampMicroseconds:   uint64(sampleTime.UnixMicro()),
				SamplingIntervalMicroseconds: 1_000_000,
				Samples:                      []uint32{42},
			}, nil
		}, c)

		stats, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit:   c.Code,
			Unit:      data.UnitMicrosecond,
			Time:      &data.TimeRange{From: sampleTime.Add(-5 * time.Minute), To: sampleTime.Add(5 * time.Minute)},
			MaxPoints: 1,
		})
		require.NoError(t, err)
		require.Len(t, stats, 1)
		assert.Equal(t, c.Code, stats[0].Circuit)
		assert.Equal(t, float64(42), stats[0].RTTMean)
		assert.Equal(t, float64(42), stats[0].RTTMin)
		assert.Equal(t, float64(42), stats[0].RTTMax)
	})

	t.Run("time range multiple buckets (MaxPoints>1)", func(t *testing.T) {
		t.Parallel()
		c := defaultCircuit()
		start := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				StartTimestampMicroseconds:   uint64(start.UnixMicro()),
				SamplingIntervalMicroseconds: 60 * 1_000_000, // 1 minute
				Samples:                      []uint32{10, 20, 30, 40, 50},
			}, nil
		}, c)

		stats, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit:   c.Code,
			Unit:      data.UnitMicrosecond,
			Time:      &data.TimeRange{From: start, To: start.Add(5 * time.Minute)},
			MaxPoints: 2,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(stats), 2)
	})

	t.Run("epoch range aggregates across partitions", func(t *testing.T) {
		t.Parallel()
		c := defaultCircuit()
		p := newTestProvider(t, func(epoch uint64) (*telemetry.DeviceLatencySamples, error) {
			base := time.Date(2023, 1, 1, 0, 0, int(epoch), 0, time.UTC)
			return &telemetry.DeviceLatencySamples{
				StartTimestampMicroseconds:   uint64(base.UnixMicro()),
				SamplingIntervalMicroseconds: 1_000_000,
				Samples:                      []uint32{10, 20, 30},
			}, nil
		}, c)

		stats, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit:   c.Code,
			Unit:      data.UnitMicrosecond,
			Epochs:    &data.EpochRange{From: 1, To: 2},
			MaxPoints: 1,
		})
		require.NoError(t, err)
		require.Len(t, stats, 1)
		assert.Equal(t, float64(10), stats[0].RTTMin)
		assert.InDelta(t, 20.0, stats[0].RTTMean, 0.001)
		assert.Equal(t, float64(30), stats[0].RTTMax)
	})

	t.Run("invalid unit", func(t *testing.T) {
		t.Parallel()
		c := defaultCircuit()
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) { return &telemetry.DeviceLatencySamples{}, nil }, c)

		_, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit:   c.Code,
			Unit:      "invalid",
			Time:      &data.TimeRange{From: time.Unix(0, 0), To: time.Unix(10, 0)},
			MaxPoints: 1,
		})
		require.Error(t, err)
	})

	t.Run("millisecond conversion", func(t *testing.T) {
		t.Parallel()
		c := defaultCircuit()
		start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				StartTimestampMicroseconds:   uint64(start.UnixMicro()),
				SamplingIntervalMicroseconds: 1_000_000,
				Samples:                      []uint32{10_000, 20_000, 30_000}, // µs
			}, nil
		}, c)

		stats, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit:   c.Code,
			Unit:      data.UnitMillisecond,
			Time:      &data.TimeRange{From: start, To: start.Add(3 * time.Second)},
			MaxPoints: 1,
		})
		require.NoError(t, err)
		require.Len(t, stats, 1)
		assert.Equal(t, float64(10), stats[0].RTTMin)
		assert.InDelta(t, 20.0, stats[0].RTTMean, 0.001)
		assert.Equal(t, float64(30), stats[0].RTTMax)
	})

	t.Run("bad/no ranges", func(t *testing.T) {
		t.Parallel()
		c := defaultCircuit()
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) { return &telemetry.DeviceLatencySamples{}, nil }, c)

		// no range
		_, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond, MaxPoints: 1,
		})
		require.Error(t, err)

		// epochs: from > to
		_, err = p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond,
			Epochs: &data.EpochRange{From: 5, To: 4}, MaxPoints: 1,
		})
		require.Error(t, err)

		// time: from after to
		from, to := time.Unix(20, 0), time.Unix(10, 0)
		_, err = p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond,
			Time: &data.TimeRange{From: from, To: to}, MaxPoints: 1,
		})
		require.Error(t, err)

		// both epochs and time set
		_, err = p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond,
			Epochs:    &data.EpochRange{From: 1, To: 2},
			Time:      &data.TimeRange{From: time.Unix(0, 0), To: time.Unix(10, 0)},
			MaxPoints: 1,
		})
		require.Error(t, err)
	})

	t.Run("time range with interval buckets", func(t *testing.T) {
		t.Parallel()
		c := defaultCircuit()
		start := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				StartTimestampMicroseconds:   uint64(start.UnixMicro()),
				SamplingIntervalMicroseconds: 60 * 1_000_000,
				Samples:                      []uint32{10_000, 20_000, 30_000, 40_000, 50_000},
			}, nil
		}, c)

		statsOut, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit:  c.Code,
			Unit:     data.UnitMicrosecond,
			Time:     &data.TimeRange{From: start, To: start.Add(5 * time.Minute)},
			Interval: 2 * time.Minute,
		})
		require.NoError(t, err)
		require.Len(t, statsOut, 2) // [0..2m): 10k,20k; [2..4m): 30k,40k,50k (last clamped)

		assert.InDelta(t, 15_000.0, statsOut[0].RTTMean, 1e-9)
		assert.InDelta(t, (30_000.0+40_000.0+50_000.0)/3.0, statsOut[1].RTTMean, 1e-9)
	})

	t.Run("time range with interval + millisecond unit conversion", func(t *testing.T) {
		t.Parallel()
		c := defaultCircuit()
		start := time.Date(2023, 1, 1, 13, 0, 0, 0, time.UTC)
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				StartTimestampMicroseconds:   uint64(start.UnixMicro()),
				SamplingIntervalMicroseconds: 60 * 1_000_000,
				Samples:                      []uint32{10_000, 20_000, 30_000, 40_000, 50_000},
			}, nil
		}, c)

		statsOut, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit:  c.Code,
			Unit:     data.UnitMillisecond, // conversion after aggregation
			Time:     &data.TimeRange{From: start, To: start.Add(5 * time.Minute)},
			Interval: 2 * time.Minute,
		})
		require.NoError(t, err)
		require.Len(t, statsOut, 2)

		// Converted to ms
		near := func(got, want float64) { assert.InDelta(t, want, got, 1e-9) }
		near(statsOut[0].RTTMean, 15.0)
		near(statsOut[0].RTTMin, 10.0)
		near(statsOut[0].RTTMax, 20.0)

		near(statsOut[1].RTTMean, (30.0+40.0+50.0)/3.0)
		near(statsOut[1].RTTMin, 30.0)
		near(statsOut[1].RTTMax, 50.0)
	})

	t.Run("interval provided but MaxPoints=1 still aggregates single point (Aggregate precedence)", func(t *testing.T) {
		t.Parallel()
		c := defaultCircuit()
		start := time.Date(2023, 1, 1, 14, 0, 0, 0, time.UTC)
		p := newTestProvider(t, func(uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{
				StartTimestampMicroseconds:   uint64(start.UnixMicro()),
				SamplingIntervalMicroseconds: 60 * 1_000_000,
				Samples:                      []uint32{1_000, 2_000, 3_000}, // µs
			}, nil
		}, c)

		statsOut, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit:   c.Code,
			Unit:      data.UnitMicrosecond,
			Time:      &data.TimeRange{From: start, To: start.Add(3 * time.Minute)},
			MaxPoints: 1,               // per Aggregate, this wins over interval
			Interval:  2 * time.Minute, // set but should be ignored by Aggregate when MaxPoints==1
		})
		require.NoError(t, err)
		require.Len(t, statsOut, 1)
		assert.InDelta(t, (1000+2000+3000)/3.0, statsOut[0].RTTMean, 1e-9)
		assert.Equal(t, float64(1000), statsOut[0].RTTMin)
		assert.Equal(t, float64(3000), statsOut[0].RTTMax)
	})

}

func defaultCircuit() data.Circuit {
	pkA := solana.NewWallet().PublicKey()
	pkB := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()
	devA := serviceability.Device{Code: "A"}
	devB := serviceability.Device{Code: "B"}
	link := serviceability.Link{Code: "L1"}
	return data.Circuit{
		Code:         circuitKey(devA.Code, devB.Code, linkPK),
		OriginDevice: data.Device{PK: pkA, Code: devA.Code},
		TargetDevice: data.Device{PK: pkB, Code: devB.Code},
		Link:         data.Link{PK: linkPK, Code: link.Code},
	}
}

func circuitKey(origin, target string, link solana.PublicKey) string {
	linkPKStr := link.String()
	shortLinkPK := linkPKStr[len(linkPKStr)-7:]
	return fmt.Sprintf("%s → %s (%s)", origin, target, shortLinkPK)
}

func newTestProvider(t *testing.T, getFunc func(epoch uint64) (*telemetry.DeviceLatencySamples, error), circuit data.Circuit) data.Provider {
	contributorPK := solana.NewWallet().PublicKey()
	p, err := data.NewProvider(&data.ProviderConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ServiceabilityClient: &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Devices: []serviceability.Device{
						{Code: circuit.OriginDevice.Code, PubKey: circuit.OriginDevice.PK},
						{Code: circuit.TargetDevice.Code, PubKey: circuit.TargetDevice.PK},
					},
					Links: []serviceability.Link{
						{Code: circuit.Link.Code, SideAPubKey: circuit.OriginDevice.PK, SideZPubKey: circuit.TargetDevice.PK, PubKey: circuit.Link.PK, ContributorPubKey: contributorPK},
					},
					Contributors: []serviceability.Contributor{
						{Code: circuit.Link.ContributorCode, PubKey: contributorPK},
					},
				}, nil
			},
		},
		TelemetryClient: &mockTelemetryClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error) {
				return getFunc(epoch)
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
