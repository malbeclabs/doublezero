package internettelemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	telemetry "github.com/malbeclabs/doublezero/sdk/telemetry/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestMonitor_InternetTelemetry_Watcher_NewAndName(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 1}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) { return &serviceability.ProgramData{}, nil }}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetInternetLatencySamplesFunc: func(context.Context, solana.PublicKey, string, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{Samples: []uint32{}}, nil
		}}

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.NotNil(t, w)
	require.Equal(t, watcherName, w.Name())
}

func TestMonitor_InternetTelemetry_Watcher_RunStopsOnCancel(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 1}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) { return &serviceability.ProgramData{}, nil }}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetInternetLatencySamplesFunc: func(context.Context, solana.PublicKey, string, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{Samples: []uint32{}}, nil
		}}

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not stop after cancel")
	}
}

func TestMonitor_InternetTelemetry_Watcher_Tick_NoCircuits(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 9}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) { return &serviceability.ProgramData{}, nil }}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetInternetLatencySamplesFunc: func(context.Context, solana.PublicKey, string, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{Samples: []uint32{}}, nil
		}}

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)

	require.NoError(t, w.Tick(context.Background()))
	w.mu.RLock()
	defer w.mu.RUnlock()
	require.False(t, w.epochSet)
	require.Equal(t, uint64(0), w.lastEpoch)
	require.Empty(t, w.stats)
}

func TestMonitor_InternetTelemetry_Watcher_Tick_ErrorFromGetProgramData(t *testing.T) {
	t.Parallel()

	cfg, reg := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 9}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) { return nil, errors.New("boom") }}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetInternetLatencySamplesFunc: func(context.Context, solana.PublicKey, string, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{Samples: []uint32{}}, nil
		}}

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.Error(t, w.Tick(context.Background()))

	require.Equal(t, 1.0, counterTotal(t, reg, MetricNameErrors)) // MetricErrorTypeGetCircuits
}

func TestMonitor_InternetTelemetry_Watcher_Tick_ErrorFromGetEpochInfo(t *testing.T) {
	t.Parallel()

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()

	cfg, reg := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return nil, errors.New("epoch fail")
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData("OR-A", "TG-A", origin, target), nil
		}}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetInternetLatencySamplesFunc: func(context.Context, solana.PublicKey, string, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{Samples: []uint32{1}}, nil
		}}

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.Error(t, w.Tick(context.Background()))

	// exactly one error increment (GetEpochInfo failure)
	require.Equal(t, 1.0, counterTotal(t, reg, MetricNameErrors))
}

func TestMonitor_InternetTelemetry_Watcher_Tick_ErrorFromGetInternetLatencySamples(t *testing.T) {
	t.Parallel()

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()

	cfg, reg := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 10}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData("OR-A", "TG-A", origin, target), nil
		}}
	// Fail only one provider/direction to make the expected count 1.
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetInternetLatencySamplesFunc: func(_ context.Context, _ solana.PublicKey, provider string, o, t solana.PublicKey, _ uint64) (*telemetry.InternetLatencySamples, error) {
			if provider == "ripeatlas" && o == origin && t == target {
				return nil, errors.New("telemetry fail")
			}
			return &telemetry.InternetLatencySamples{Samples: []uint32{1}}, nil
		}}

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.Error(t, w.Tick(context.Background()))

	require.Equal(t, 1.0, counterTotal(t, reg, MetricNameErrors)) // MetricErrorTypeGetLatencySamples
}

func TestMonitor_InternetTelemetry_Watcher_Tick_SameEpoch_EmitsMetricDeltas_Aggregated(t *testing.T) {
	t.Parallel()

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	originCode, targetCode := "OR-A", "TG-A"
	var step int32

	cfg, reg := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 10}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData(originCode, targetCode, origin, target), nil
		}}
	cfg.Telemetry = providerStepTelemetryMock(origin, target, &step)

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)

	// First tick seeds state; no deltas
	require.NoError(t, w.Tick(context.Background()))
	require.Equal(t, 0.0, counterTotal(t, reg, MetricNameSuccesses))
	require.Equal(t, 0.0, counterTotal(t, reg, MetricNameLosses))
	require.Equal(t, 0.0, counterTotal(t, reg, MetricNameSamples))

	// Second tick (same epoch) emits deltas across providers:
	// forward +1 success (ripeatlas), reverse +1 success +1 loss (wheresitup) → totals: succ=2, loss=1, samples=3
	atomic.StoreInt32(&step, 1)
	require.NoError(t, w.Tick(context.Background()))
	require.Equal(t, 2.0, counterTotal(t, reg, MetricNameSuccesses))
	require.Equal(t, 1.0, counterTotal(t, reg, MetricNameLosses))
	require.Equal(t, 3.0, counterTotal(t, reg, MetricNameSamples))
}

func TestMonitor_InternetTelemetry_Watcher_Tick_EpochRollover_NoMetricDeltas(t *testing.T) {
	t.Parallel()

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	originCode, targetCode := "OR-A", "TG-A"

	epochVal := uint64(10)

	cfg, reg := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: epochVal}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData(originCode, targetCode, origin, target), nil
		}}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetInternetLatencySamplesFunc: func(_ context.Context, _ solana.PublicKey, provider string, o, t solana.PublicKey, e uint64) (*telemetry.InternetLatencySamples, error) {
			if e == 10 {
				if provider == "ripeatlas" {
					return &telemetry.InternetLatencySamples{Samples: []uint32{1, 2, 0, 5}}, nil // 3/1
				}
				if provider == "wheresitup" {
					return &telemetry.InternetLatencySamples{Samples: []uint32{0, 0, 7, 8}}, nil // 2/2
				}
			}
			if provider == "ripeatlas" {
				return &telemetry.InternetLatencySamples{Samples: []uint32{8, 8, 0}}, nil // 2/1
			}
			if provider == "wheresitup" {
				return &telemetry.InternetLatencySamples{Samples: []uint32{4, 5, 0, 6, 7}}, nil // 4/1
			}
			return &telemetry.InternetLatencySamples{Samples: []uint32{0}}, nil
		}}

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)

	// Baseline at epoch 10
	require.NoError(t, w.Tick(context.Background()))
	// Rollover to epoch 11 → no deltas emitted for new epoch
	epochVal = 11
	require.NoError(t, w.Tick(context.Background()))

	require.Equal(t, 0.0, counterTotal(t, reg, MetricNameSuccesses))
	require.Equal(t, 0.0, counterTotal(t, reg, MetricNameLosses))
	require.Equal(t, 0.0, counterTotal(t, reg, MetricNameSamples))
}

func TestMonitor_InternetTelemetry_Watcher_Tick_MixedCircuits_ErrorBubbles(t *testing.T) {
	t.Parallel()

	a := solana.NewWallet().PublicKey()
	b := solana.NewWallet().PublicKey()

	cfg, _ := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 42}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData("A", "B", a, b), nil
		}}
	cfg.Telemetry = &mockTelemetryProgramClient{
		// succeed for one provider, fail for the other
		GetInternetLatencySamplesFunc: func(_ context.Context, _ solana.PublicKey, provider string, _, _ solana.PublicKey, _ uint64) (*telemetry.InternetLatencySamples, error) {
			if provider == "ripeatlas" {
				return &telemetry.InternetLatencySamples{Samples: []uint32{1, 2, 3}}, nil
			}
			return nil, errors.New("wheresitup timeout")
		}}

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.Error(t, w.Tick(context.Background()))
}

func TestMonitor_InternetTelemetry_Watcher_Run_ContinuesAfterTickError(t *testing.T) {
	t.Parallel()

	var step atomic.Int32 // 0=failing, 1=success

	cfg, _ := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 777}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			if step.Load() == 0 {
				return nil, errors.New("boom")
			}
			return &serviceability.ProgramData{}, nil
		}}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetInternetLatencySamplesFunc: func(context.Context, solana.PublicKey, string, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{Samples: []uint32{}}, nil
		}}

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()

	time.Sleep(8 * time.Millisecond) // first Tick should fail
	step.Store(1)                    // subsequent Ticks succeed
	time.Sleep(8 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not stop after cancel")
	}

	w.mu.RLock()
	defer w.mu.RUnlock()
	require.False(t, w.epochSet)
	require.Empty(t, w.stats)
}

func TestMonitor_InternetTelemetry_Watcher_Tick_EmptySamples_WritesZeroStatsAndSetsEpoch(t *testing.T) {
	t.Parallel()

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	originCode, targetCode := "OR-Z", "TG-Z"

	cfg, _ := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 77}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData(originCode, targetCode, origin, target), nil
		}}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetInternetLatencySamplesFunc: func(context.Context, solana.PublicKey, string, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.InternetLatencySamples, error) {
			return &telemetry.InternetLatencySamples{Samples: []uint32{}}, nil
		}}

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)

	require.NoError(t, w.Tick(context.Background()))

	keyFwd := "epoch=77, data_provider=ripeatlas, circuit=" + circuitKey(originCode, targetCode)
	keyRev := "epoch=77, data_provider=ripeatlas, circuit=" + circuitKey(targetCode, originCode)
	// Note: depending on your watcher’s formatting, adjust keys if needed or skip these if you only care about metrics.

	w.mu.RLock()
	defer w.mu.RUnlock()
	require.True(t, w.epochSet)
	require.Equal(t, uint64(77), w.lastEpoch)
	require.Equal(t, CircuitTelemetryStats{SuccessCount: 0, LossCount: 0}, w.stats[keyFwd])
	require.Equal(t, CircuitTelemetryStats{SuccessCount: 0, LossCount: 0}, w.stats[keyRev])
}

func TestWatcher_Tick_AccountNotFound_IncrementsAccountNotFoundMetric(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)

	a := solana.NewWallet().PublicKey()
	b := solana.NewWallet().PublicKey()

	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 5}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData("A", "B", a, b), nil
		}}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetInternetLatencySamplesFunc: func(ctx context.Context, _ solana.PublicKey, provider string, _, _ solana.PublicKey, _ uint64) (*telemetry.InternetLatencySamples, error) {
			return nil, telemetry.ErrAccountNotFound
		}}

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.NoError(t, w.Tick(context.Background()))

	require.Equal(t, 1.0, testutil.ToFloat64(cfg.Metrics.AccountNotFound.WithLabelValues("ripeatlas", "A → B")))
}

func TestMonitor_InternetTelemetry_Watcher_Tick_DeletesMetrics_WhenCircuitDisappears(t *testing.T) {
	t.Parallel()

	cfg, reg := baseCfg(t)

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	originCode, targetCode := "OR-A", "TG-A"
	code := circuitKey(originCode, targetCode) // label value for circuit

	var step int32    // 0=seed, 1=emit deltas
	var present int32 // 0=absent, 1=present
	atomic.StoreInt32(&present, 1)

	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 10}, nil
		},
	}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			if atomic.LoadInt32(&present) == 0 {
				return &serviceability.ProgramData{}, nil
			}
			return makeProgramData(originCode, targetCode, origin, target), nil
		},
	}
	cfg.Telemetry = providerStepTelemetryMock(origin, target, &step)

	w, err := NewInternetTelemetryWatcher(cfg)
	require.NoError(t, err)

	// Tick 1: circuits present, baseline only → no deltas yet
	require.NoError(t, w.Tick(context.Background()))
	require.Equal(t, 0.0, counterTotal(t, reg, MetricNameSuccesses))
	require.Equal(t, 0.0, counterTotal(t, reg, MetricNameLosses))
	require.Equal(t, 0.0, counterTotal(t, reg, MetricNameSamples))

	// Tick 2: emit deltas to create series for both providers
	atomic.StoreInt32(&step, 1)
	require.NoError(t, w.Tick(context.Background()))
	require.Greater(t, counterTotal(t, reg, MetricNameSuccesses), 0.0)
	require.Greater(t, counterTotal(t, reg, MetricNameSamples), 0.0)
	require.True(t, hasMetricWithLabelValues(t, reg, MetricNameSuccesses, "ripeatlas", code))
	require.True(t, hasMetricWithLabelValues(t, reg, MetricNameSuccesses, "wheresitup", code))

	// Tick 3: circuits disappear → metrics for that circuit should be deleted (both providers)
	atomic.StoreInt32(&present, 0)
	require.NoError(t, w.Tick(context.Background()))

	require.False(t, hasMetricWithLabelValues(t, reg, MetricNameSuccesses, "ripeatlas", code))
	require.False(t, hasMetricWithLabelValues(t, reg, MetricNameSuccesses, "wheresitup", code))
	require.False(t, hasMetricWithLabelValues(t, reg, MetricNameLosses, "ripeatlas", code))
	require.False(t, hasMetricWithLabelValues(t, reg, MetricNameLosses, "wheresitup", code))
	require.False(t, hasMetricWithLabelValues(t, reg, MetricNameSamples, "ripeatlas", code))
	require.False(t, hasMetricWithLabelValues(t, reg, MetricNameSamples, "wheresitup", code))
	require.False(t, hasMetricWithLabelValues(t, reg, MetricNameAccountNotFound, "ripeatlas", code))
	require.False(t, hasMetricWithLabelValues(t, reg, MetricNameAccountNotFound, "wheresitup", code))

	// Internal stats entries for this circuit should be scrubbed
	w.mu.RLock()
	for k := range w.stats {
		require.NotContains(t, k, ", circuit="+code)
	}
	w.mu.RUnlock()
}

func hasMetricWithLabelValues(t *testing.T, g prometheus.Gatherer, metric, labelA, labelB string) bool {
	mfs, err := g.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != metric {
			continue
		}
		for _, m := range mf.GetMetric() {
			var seenA, seenB bool
			for _, lp := range m.GetLabel() {
				if lp.GetValue() == labelA {
					seenA = true
				}
				if lp.GetValue() == labelB {
					seenB = true
				}
			}
			if seenA && seenB {
				return true
			}
		}
	}
	return false
}

type mockLedgerRPC struct {
	GetEpochInfoFunc func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
}

func (m *mockLedgerRPC) GetEpochInfo(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
	return m.GetEpochInfoFunc(ctx, c)
}

type mockServiceabilityClient struct {
	GetProgramDataFunc func(ctx context.Context) (*serviceability.ProgramData, error)
}

func (m *mockServiceabilityClient) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return m.GetProgramDataFunc(ctx)
}

type mockTelemetryProgramClient struct {
	GetInternetLatencySamplesFunc func(ctx context.Context, collectorOraclePK solana.PublicKey, dataProviderName string, originLocationPK, targetLocationPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error)
}

func (m *mockTelemetryProgramClient) GetInternetLatencySamples(ctx context.Context, collectorOraclePK solana.PublicKey, dataProviderName string, originLocationPK, targetLocationPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error) {
	return m.GetInternetLatencySamplesFunc(ctx, collectorOraclePK, dataProviderName, originLocationPK, targetLocationPK, epoch)
}

func newTestLogger(t *testing.T) *slog.Logger {
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log = log.With("test", t.Name())
	return log
}

func circuitKey(originCode, targetCode string) string {
	return fmt.Sprintf("%s → %s", originCode, targetCode)
}

func makeProgramData(exchangeCode1, exchangeCode2 string, exchangePK1, exchangePK2 solana.PublicKey) *serviceability.ProgramData {
	return &serviceability.ProgramData{
		Exchanges: []serviceability.Exchange{
			{Code: exchangeCode1, PubKey: pkAsBytes(exchangePK1)},
			{Code: exchangeCode2, PubKey: pkAsBytes(exchangePK2)},
		},
	}
}

func pkAsBytes(pk solana.PublicKey) (out [32]byte) {
	copy(out[:], pk[:])
	return
}

func newTestMetrics() (*prometheus.Registry, *Metrics) {
	reg := prometheus.NewRegistry()
	m := NewMetrics()
	m.Register(reg)
	return reg, m
}

func baseCfg(t *testing.T) (*Config, *prometheus.Registry) {
	reg, metrics := newTestMetrics()
	return &Config{
		Logger:   newTestLogger(t),
		Metrics:  metrics,
		Interval: 5 * time.Millisecond,
		// InternetLatencyCollectorPK is required by the watcher; set a dummy.
		InternetLatencyCollectorPK: solana.NewWallet().PublicKey(),
	}, reg
}

// providerStepTelemetryMock simulates two providers ("ripeatlas" and "wheresitup")
// and returns per-provider forward-direction samples. Step 0 seeds a baseline; step 1 adds deltas:
// - ripeatlas forward: +1 success
// - wheresitup forward: +1 success and +1 loss
func providerStepTelemetryMock(origin, target solana.PublicKey, step *int32) *mockTelemetryProgramClient {
	return &mockTelemetryProgramClient{
		GetInternetLatencySamplesFunc: func(_ context.Context, _ solana.PublicKey, provider string, o, t solana.PublicKey, _ uint64) (*telemetry.InternetLatencySamples, error) {
			// Only the forward circuit is exercised by the watcher.
			if o != origin || t != target {
				return &telemetry.InternetLatencySamples{Samples: nil}, nil
			}
			switch provider {
			case "ripeatlas":
				if atomic.LoadInt32(step) == 0 {
					return &telemetry.InternetLatencySamples{Samples: []uint32{1, 2, 0, 5}}, nil
				} // 3/1
				return &telemetry.InternetLatencySamples{Samples: []uint32{1, 2, 0, 5, 9}}, nil // +1 success (4/1)
			case "wheresitup":
				if atomic.LoadInt32(step) == 0 {
					return &telemetry.InternetLatencySamples{Samples: []uint32{0, 0, 7}}, nil
				} // 1/2
				return &telemetry.InternetLatencySamples{Samples: []uint32{0, 0, 7, 3, 0}}, nil // +1 success, +1 loss (2/3)
			default:
				return &telemetry.InternetLatencySamples{Samples: nil}, nil
			}
		},
	}
}

// counterTotal sums a metric family across all labels.
func counterTotal(t *testing.T, g prometheus.Gatherer, metric string) float64 {
	mfs, err := g.Gather()
	require.NoError(t, err)
	var total float64
	for _, mf := range mfs {
		if mf.GetName() != metric {
			continue
		}
		for _, m := range mf.GetMetric() {
			if c := m.GetCounter(); c != nil {
				total += c.GetValue()
			}
		}
	}
	return total
}
