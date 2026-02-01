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
	data "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/internet"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInternetProvider_GetCircuitLatencies(t *testing.T) {
	t.Parallel()

	const provA = data.DataProviderNameRIPEAtlas
	const provB = data.DataProviderNameWheresitup

	t.Run("requires data provider", func(t *testing.T) {
		t.Parallel()
		c := defaultInternetCircuit()
		p := newInternetTestProvider(t, func(string, uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{}, nil
		}, c)

		_, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond, Epochs: &data.EpochRange{From: 1, To: 1}, MaxPoints: 1,
			// DataProvider omitted
		})
		require.Error(t, err)
	})

	t.Run("epoch cache hit is per dataProvider", func(t *testing.T) {
		t.Parallel()
		c := defaultInternetCircuit()
		var calls int32
		p := newInternetTestProvider(t, func(provider string, _ uint64) (*telemetry.InternetLatencySamples, error) {
			atomic.AddInt32(&calls, 1)
			return &telemetry.InternetLatencySamples{
				InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
					StartTimestampMicroseconds:   1_700_000_000_000_000,
					SamplingIntervalMicroseconds: 1_000_000,
				},
				Samples: []uint32{10, 20},
			}, nil
		}, c)

		cfg := data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond, Epochs: &data.EpochRange{From: 1, To: 1}, MaxPoints: 2,
			DataProvider: provA,
		}

		stats1, err := p.GetCircuitLatencies(context.Background(), cfg)
		require.NoError(t, err)
		require.NotEmpty(t, stats1)

		stats2, err := p.GetCircuitLatencies(context.Background(), cfg)
		require.NoError(t, err)
		require.NotEmpty(t, stats2)

		assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "second call should be served from cache for same provider")
	})

	t.Run("different dataProviders have independent caches", func(t *testing.T) {
		t.Parallel()
		c := defaultInternetCircuit()
		var calls int32
		p := newInternetTestProvider(t, func(provider string, _ uint64) (*telemetry.InternetLatencySamples, error) {
			atomic.AddInt32(&calls, 1)
			return &telemetry.InternetLatencySamples{
				InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
					StartTimestampMicroseconds:   1_700_000_000_000_000,
					SamplingIntervalMicroseconds: 1_000_000,
				},
				Samples: []uint32{11, 22},
			}, nil
		}, c)

		cfgA := data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond, Epochs: &data.EpochRange{From: 1, To: 1}, MaxPoints: 1,
			DataProvider: provA,
		}
		cfgB := cfgA
		cfgB.DataProvider = provB

		_, err := p.GetCircuitLatencies(context.Background(), cfgA)
		require.NoError(t, err)
		_, err = p.GetCircuitLatencies(context.Background(), cfgB)
		require.NoError(t, err)

		assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "each provider should fetch once")
	})

	t.Run("epoch account not found is skipped and returns empty (and short-caches internally)", func(t *testing.T) {
		t.Parallel()
		c := defaultInternetCircuit()
		var calls int32
		p := newInternetTestProvider(t, func(provider string, _ uint64) (*telemetry.InternetLatencySamples, error) {
			atomic.AddInt32(&calls, 1)
			return nil, telemetry.ErrAccountNotFound
		}, c)

		cfg := data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond,
			Epochs:    &data.EpochRange{From: 1, To: 1},
			MaxPoints: 1, DataProvider: data.DataProviderNameRIPEAtlas,
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
		c := defaultInternetCircuit()
		sample := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		p := newInternetTestProviderWithEpochFinder(t,
			func(string, uint64) (*telemetry.InternetLatencySamples, error) {
				return &telemetry.InternetLatencySamples{
					InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
						StartTimestampMicroseconds:   uint64(sample.UnixMicro()),
						SamplingIntervalMicroseconds: 1_000_000,
					},
					Samples: []uint32{42},
				}, nil
			},
			c,
			func(_ context.Context, _ time.Time) (uint64, error) { return 10, nil },
		)

		stats, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond,
			Time:      &data.TimeRange{From: sample.Add(-2 * time.Minute), To: sample.Add(2 * time.Minute)},
			MaxPoints: 1, DataProvider: provA,
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
		c := defaultInternetCircuit()
		start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		p := newInternetTestProviderWithEpochFinder(t,
			func(string, uint64) (*telemetry.InternetLatencySamples, error) {
				return &telemetry.InternetLatencySamples{
					InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
						StartTimestampMicroseconds:   uint64(start.UnixMicro()),
						SamplingIntervalMicroseconds: 60 * 1_000_000, // 1 min
					},
					Samples: []uint32{10, 20, 30, 40, 50},
				}, nil
			},
			c,
			func(_ context.Context, target time.Time) (uint64, error) {
				if target.Before(start.Add(3 * time.Minute)) {
					return 10, nil
				}
				return 11, nil
			},
		)

		stats, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond,
			Time:      &data.TimeRange{From: start, To: start.Add(5 * time.Minute)},
			MaxPoints: 2, DataProvider: provA,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(stats), 2)
	})

	t.Run("epoch range aggregates across partitions", func(t *testing.T) {
		t.Parallel()
		c := defaultInternetCircuit()
		p := newInternetTestProvider(t, func(_ string, epoch uint64) (*telemetry.InternetLatencySamples, error) {
			base := time.Date(2024, 1, 1, 0, 0, int(epoch), 0, time.UTC)
			return &telemetry.InternetLatencySamples{
				InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
					StartTimestampMicroseconds:   uint64(base.UnixMicro()),
					SamplingIntervalMicroseconds: 1_000_000,
				},
				Samples: []uint32{10, 20, 30},
			}, nil
		}, c)

		stats, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond,
			Epochs: &data.EpochRange{From: 1, To: 2}, MaxPoints: 1, DataProvider: provA,
		})
		require.NoError(t, err)
		require.Len(t, stats, 1)
		assert.Equal(t, float64(10), stats[0].RTTMin)
		assert.InDelta(t, 20.0, stats[0].RTTMean, 0.001)
		assert.Equal(t, float64(30), stats[0].RTTMax)
	})

	t.Run("invalid unit", func(t *testing.T) {
		t.Parallel()
		c := defaultInternetCircuit()
		p := newInternetTestProvider(t, func(string, uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{}, nil
		}, c)

		_, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: "invalid",
			Epochs: &data.EpochRange{From: 1, To: 1}, MaxPoints: 1, DataProvider: provA,
		})
		require.Error(t, err)
	})

	t.Run("millisecond conversion", func(t *testing.T) {
		t.Parallel()
		c := defaultInternetCircuit()
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		p := newInternetTestProvider(t, func(string, uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{
				InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
					StartTimestampMicroseconds:   uint64(start.UnixMicro()),
					SamplingIntervalMicroseconds: 1_000_000,
				},
				Samples: []uint32{10_000, 20_000, 30_000}, // µs
			}, nil
		}, c)

		stats, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMillisecond,
			Time:      &data.TimeRange{From: start, To: start.Add(3 * time.Second)},
			MaxPoints: 1, DataProvider: provA,
		})
		require.NoError(t, err)
		require.Len(t, stats, 1)
		assert.Equal(t, float64(10), stats[0].RTTMin)
		assert.InDelta(t, 20.0, stats[0].RTTMean, 0.001)
		assert.Equal(t, float64(30), stats[0].RTTMax)
	})

	t.Run("bad/no ranges and overlaps", func(t *testing.T) {
		t.Parallel()
		c := defaultInternetCircuit()
		p := newInternetTestProvider(t, func(string, uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{}, nil
		}, c)

		_, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond, MaxPoints: 1, DataProvider: provA,
		})
		require.Error(t, err)

		_, err = p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond,
			Epochs: &data.EpochRange{From: 5, To: 4}, MaxPoints: 1, DataProvider: provA,
		})
		require.Error(t, err)

		from, to := time.Unix(20, 0), time.Unix(10, 0)
		_, err = p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond,
			Time: &data.TimeRange{From: from, To: to}, MaxPoints: 1, DataProvider: provA,
		})
		require.Error(t, err)

		_, err = p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond,
			Epochs:    &data.EpochRange{From: 1, To: 2},
			Time:      &data.TimeRange{From: time.Unix(0, 0), To: time.Unix(10, 0)},
			MaxPoints: 1, DataProvider: provA,
		})
		require.Error(t, err)
	})

	t.Run("time range with interval buckets", func(t *testing.T) {
		t.Parallel()
		const prov = data.DataProviderNameRIPEAtlas
		c := defaultInternetCircuit()
		start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

		p := newInternetTestProviderWithEpochFinder(t,
			func(_ string, _ uint64) (*telemetry.InternetLatencySamples, error) {
				return &telemetry.InternetLatencySamples{
					InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
						StartTimestampMicroseconds:   uint64(start.UnixMicro()),
						SamplingIntervalMicroseconds: 60 * 1_000_000, // 1 min
					},
					Samples: []uint32{10_000, 20_000, 30_000, 40_000, 50_000}, // µs
				}, nil
			},
			c,
			// Keep both from/to in the same epoch for simplicity.
			func(_ context.Context, _ time.Time) (uint64, error) { return 10, nil },
		)

		out, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond, DataProvider: prov,
			Time:     &data.TimeRange{From: start, To: start.Add(5 * time.Minute)},
			Interval: 2 * time.Minute,
		})
		require.NoError(t, err)
		require.Len(t, out, 2) // [0..2m): 10k,20k; [2..4m): 30k,40k,50k (last clamped)

		assert.InDelta(t, 15_000.0, out[0].RTTMean, 1e-9)
		assert.InDelta(t, (30_000.0+40_000.0+50_000.0)/3.0, out[1].RTTMean, 1e-9)
	})

	t.Run("time range with interval + millisecond unit conversion", func(t *testing.T) {
		t.Parallel()
		const prov = data.DataProviderNameRIPEAtlas
		c := defaultInternetCircuit()
		start := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)

		p := newInternetTestProviderWithEpochFinder(t,
			func(_ string, _ uint64) (*telemetry.InternetLatencySamples, error) {
				return &telemetry.InternetLatencySamples{
					InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
						StartTimestampMicroseconds:   uint64(start.UnixMicro()),
						SamplingIntervalMicroseconds: 60 * 1_000_000,
					},
					Samples: []uint32{10_000, 20_000, 30_000, 40_000, 50_000}, // µs
				}, nil
			},
			c,
			func(_ context.Context, _ time.Time) (uint64, error) { return 10, nil },
		)

		out, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMillisecond, DataProvider: prov, // convert after aggregation
			Time:     &data.TimeRange{From: start, To: start.Add(5 * time.Minute)},
			Interval: 2 * time.Minute,
		})
		require.NoError(t, err)
		require.Len(t, out, 2)

		// Converted to ms
		assert.InDelta(t, 15.0, out[0].RTTMean, 1e-9)
		assert.InDelta(t, 10.0, out[0].RTTMin, 1e-9)
		assert.InDelta(t, 20.0, out[0].RTTMax, 1e-9)

		assert.InDelta(t, (30.0+40.0+50.0)/3.0, out[1].RTTMean, 1e-9)
		assert.InDelta(t, 30.0, out[1].RTTMin, 1e-9)
		assert.InDelta(t, 50.0, out[1].RTTMax, 1e-9)
	})

	t.Run("interval provided but MaxPoints=1 still aggregates single point", func(t *testing.T) {
		t.Parallel()
		const prov = data.DataProviderNameRIPEAtlas
		c := defaultInternetCircuit()
		start := time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC)

		p := newInternetTestProviderWithEpochFinder(t,
			func(_ string, _ uint64) (*telemetry.InternetLatencySamples, error) {
				return &telemetry.InternetLatencySamples{
					InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
						StartTimestampMicroseconds:   uint64(start.UnixMicro()),
						SamplingIntervalMicroseconds: 60 * 1_000_000,
					},
					Samples: []uint32{1_000, 2_000, 3_000}, // µs
				}, nil
			},
			c,
			func(_ context.Context, _ time.Time) (uint64, error) { return 10, nil },
		)

		out, err := p.GetCircuitLatencies(context.Background(), data.GetCircuitLatenciesConfig{
			Circuit: c.Code, Unit: data.UnitMicrosecond, DataProvider: prov,
			Time:      &data.TimeRange{From: start, To: start.Add(3 * time.Minute)},
			MaxPoints: 1,               // per Aggregate, this wins
			Interval:  2 * time.Minute, // ignored when MaxPoints==1
		})
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.InDelta(t, (1000.0+2000.0+3000.0)/3.0, out[0].RTTMean, 1e-9)
		assert.Equal(t, float64(1000), out[0].RTTMin)
		assert.Equal(t, float64(3000), out[0].RTTMax)
	})

}

func defaultInternetCircuit() data.Circuit {
	// Internet circuit uses Exchanges rather than Devices.
	exA := serviceability.Exchange{Code: "YOW"} // Ottawa
	exB := serviceability.Exchange{Code: "YYC"} // Calgary
	pkA := solana.NewWallet().PublicKey()
	pkB := solana.NewWallet().PublicKey()
	return data.Circuit{
		Code:           circuitKey(exA.Code, exB.Code),
		OriginExchange: data.Exchange{PK: pkA, Code: exA.Code},
		TargetExchange: data.Exchange{PK: pkB, Code: exB.Code},
	}
}

func circuitKey(origin, target string) string {
	return fmt.Sprintf("%s → %s", origin, target)
}

func newInternetTestProvider(
	t *testing.T,
	getFunc func(dataProvider string, epoch uint64) (*telemetry.InternetLatencySamples, error),
	circuit data.Circuit,
) data.Provider {
	return newInternetTestProviderWithEpochFinder(t, getFunc, circuit,
		func(_ context.Context, _ time.Time) (uint64, error) { return 1, nil })
}

func newInternetTestProviderWithEpochFinder(
	t *testing.T,
	getFunc func(dataProvider string, epoch uint64) (*telemetry.InternetLatencySamples, error),
	circuit data.Circuit,
	approx func(ctx context.Context, target time.Time) (uint64, error),
) data.Provider {
	agentPK := solana.NewWallet().PublicKey()

	p, err := data.NewProvider(&data.ProviderConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ServiceabilityClient: &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				// Provide exchanges so GetCircuits(...) can resolve the circuit by code.
				return &serviceability.ProgramData{
					Exchanges: []serviceability.Exchange{
						{Code: circuit.OriginExchange.Code, PubKey: circuit.OriginExchange.PK},
						{Code: circuit.TargetExchange.Code, PubKey: circuit.TargetExchange.PK},
					},
				}, nil
			},
		},
		TelemetryClient: &mockTelemetryClient{
			GetInternetLatencySamplesFunc: func(ctx context.Context, dataProvider string, origin, target, agent solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error) {
				return getFunc(dataProvider, epoch)
			},
		},
		EpochFinder:                    &mockEpochFinder{ApproximateAtTimeFunc: approx},
		AgentPK:                        agentPK,
		CircuitsCacheTTL:               time.Minute,
		CurrentEpochLatenciesCacheTTL:  10 * time.Second,
		HistoricEpochLatenciesCacheTTL: time.Hour,
	})
	require.NoError(t, err)
	return p
}
