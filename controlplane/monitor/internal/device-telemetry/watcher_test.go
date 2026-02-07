package devicetelemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestWatcher_NewAndName(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 1}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) { return &serviceability.ProgramData{}, nil }}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
		}}

	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.NotNil(t, w)
	require.Equal(t, watcherName, w.Name())
}

func TestWatcher_RunStopsOnCancel(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 1}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) { return &serviceability.ProgramData{}, nil }}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
		}}

	w, err := NewDeviceTelemetryWatcher(cfg)
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

func TestWatcher_Tick_NoCircuits(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 9}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) { return &serviceability.ProgramData{}, nil }}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
		}}

	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)

	require.NoError(t, w.Tick(context.Background()))
	w.mu.RLock()
	defer w.mu.RUnlock()
	require.False(t, w.epochSet)
	require.Equal(t, uint64(0), w.lastEpoch)
	require.Empty(t, w.stats)
}

func TestWatcher_Tick_ErrorFromGetProgramData(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 9}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) { return nil, errors.New("boom") }}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
		}}

	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.Error(t, w.Tick(context.Background()))

	got := testutil.ToFloat64(cfg.Metrics.Errors.WithLabelValues(MetricErrorTypeGetCircuits))
	require.Equal(t, 1.0, got)
}

func TestWatcher_Tick_ErrorFromGetEpochInfo(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()

	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return nil, errors.New("epoch fail")
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData("OR-A", "TG-A", origin, target, link, "LK-A"), nil
		}}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{Samples: []uint32{1}}, nil
		}}

	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.Error(t, w.Tick(context.Background()))

	got := testutil.ToFloat64(cfg.Metrics.Errors.WithLabelValues(MetricErrorTypeGetEpochInfo))
	require.Equal(t, 1.0, got)
}

func TestWatcher_Tick_ErrorFromGetDeviceLatencySamples(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()

	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 10}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData("OR-A", "TG-A", origin, target, link, "LK-A"), nil
		}}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetDeviceLatencySamplesFunc: func(ctx context.Context, o, tpk, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
			if o == origin && tpk == target { // FAIL forward only
				return nil, errors.New("telemetry fail")
			}
			return &telemetry.DeviceLatencySamples{Samples: []uint32{1}}, nil // succeed reverse
		}}

	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.Error(t, w.Tick(context.Background()))

	got := testutil.ToFloat64(cfg.Metrics.Errors.WithLabelValues(MetricErrorTypeGetLatencySamples))
	require.Equal(t, 1.0, got)
}

func TestWatcher_Tick_SameEpoch_EmitsMetricDeltas_AggregatedPerLink(t *testing.T) {
	t.Parallel()

	cfg, reg := baseCfg(t)

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()
	originCode, targetCode := "OR-A", "TG-A"
	var step int32

	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 10}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData(originCode, targetCode, origin, target, link, "LK-A"), nil
		}}
	cfg.Telemetry = stepTelemetryMock(origin, target, &step)

	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)

	// Tick 1: seed only → no deltas
	require.NoError(t, w.Tick(context.Background()))
	require.Equal(t, 0.0, counterTotal(t, reg, MetricNameSuccesses))
	require.Equal(t, 0.0, counterTotal(t, reg, MetricNameLosses))
	require.Equal(t, 0.0, counterTotal(t, reg, MetricNameSamples))

	// Tick 2 same epoch: forward +1 success; reverse +1 success +1 loss (totals: succ=2, loss=1, samples=3)
	atomic.StoreInt32(&step, 1)
	require.NoError(t, w.Tick(context.Background()))
	require.Equal(t, 2.0, counterTotal(t, reg, MetricNameSuccesses))
	require.Equal(t, 1.0, counterTotal(t, reg, MetricNameLosses))
	require.Equal(t, 3.0, counterTotal(t, reg, MetricNameSamples))
}

func TestWatcher_Tick_EpochRollover_NoMetricDeltas(t *testing.T) {
	t.Parallel()

	cfg, reg := baseCfg(t)

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()
	originCode, targetCode := "OR-A", "TG-A"

	epochVal := uint64(10)
	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: epochVal}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData(originCode, targetCode, origin, target, link, "LK-A"), nil
		}}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetDeviceLatencySamplesFunc: func(ctx context.Context, o, t, l solana.PublicKey, e uint64) (*telemetry.DeviceLatencySamples, error) {
			if e == 10 {
				if o == origin && t == target {
					return &telemetry.DeviceLatencySamples{Samples: []uint32{1, 2, 0, 5}}, nil // 3/1
				}
				return &telemetry.DeviceLatencySamples{Samples: []uint32{0, 0, 7}}, nil // 1/2
			}
			if o == origin && t == target {
				return &telemetry.DeviceLatencySamples{Samples: []uint32{8, 8, 0}}, nil // 2/1
			}
			return &telemetry.DeviceLatencySamples{Samples: []uint32{0}}, nil // 0/1
		}}

	w, err := NewDeviceTelemetryWatcher(cfg)
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

func TestWatcher_Tick_MixedCircuits_ErrorBubbles(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)

	a := solana.NewWallet().PublicKey()
	b := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()

	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 42}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		// one link → two circuits (A→B and B→A)
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData("A", "B", a, b, link, "L"), nil
		}}
	cfg.Telemetry = &mockTelemetryProgramClient{
		// succeed for one direction, fail for the other
		GetDeviceLatencySamplesFunc: func(ctx context.Context, o, t, l solana.PublicKey, e uint64) (*telemetry.DeviceLatencySamples, error) {
			if o == a && t == b {
				return &telemetry.DeviceLatencySamples{Samples: []uint32{1, 2, 3}}, nil
			}
			return nil, errors.New("reflector timeout")
		}}

	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.Error(t, w.Tick(context.Background()))
}

func TestWatcher_Run_ContinuesAfterTickError(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)

	var step atomic.Int32 // 0=failing, 1=success

	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, ct solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
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
		GetDeviceLatencySamplesFunc: func(context.Context, solana.PublicKey, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
		}}

	w, err := NewDeviceTelemetryWatcher(cfg)
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

func TestWatcher_Tick_EmptySamples_WritesZeroStatsAndSetsEpoch(t *testing.T) {
	t.Parallel()

	cfg, _ := baseCfg(t)

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()
	originCode, targetCode := "OR-Z", "TG-Z"

	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, ct solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 77}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData(originCode, targetCode, origin, target, link, "LK-Z"), nil
		}}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetDeviceLatencySamplesFunc: func(context.Context, solana.PublicKey, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.DeviceLatencySamples, error) {
			return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
		}}

	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)

	require.NoError(t, w.Tick(context.Background()))

	keyFwd := "77-" + circuitKey(originCode, targetCode, link)
	keyRev := "77-" + circuitKey(targetCode, originCode, link)

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
	link := solana.NewWallet().PublicKey()

	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 5}, nil
		}}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return makeProgramData("A", "B", a, b, link, "L"), nil
		}}
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
			return nil, telemetry.ErrAccountNotFound
		}}

	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.NoError(t, w.Tick(context.Background()))

	circuitKey := circuitKey("A", "B", link)
	require.Equal(t, 1.0, testutil.ToFloat64(cfg.Metrics.AccountNotFound.WithLabelValues(circuitKey, "pending")))
}

func TestWatcher_Tick_DeletesMetrics_WhenCircuitDisappears(t *testing.T) {
	t.Parallel()

	cfg, reg := baseCfg(t)

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()
	originCode, targetCode := "OR-A", "TG-A"
	codeFwd := circuitKey(originCode, targetCode, link)
	codeRev := circuitKey(targetCode, originCode, link)

	var step int32
	var present int32
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
			return makeProgramData(originCode, targetCode, origin, target, link, "LK-A"), nil
		},
	}
	cfg.Telemetry = stepTelemetryMock(origin, target, &step)

	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)

	// Tick 1: circuits present, baseline only → no deltas yet
	require.NoError(t, w.Tick(context.Background()))
	require.False(t, hasMetricWithLabelValue(t, reg, MetricNameSuccesses, codeFwd))
	require.False(t, hasMetricWithLabelValue(t, reg, MetricNameSuccesses, codeRev))

	// Tick 2: emit deltas to create series
	atomic.StoreInt32(&step, 1)
	require.NoError(t, w.Tick(context.Background()))
	require.Greater(t, counterTotal(t, reg, MetricNameSuccesses), 0.0)
	require.Greater(t, counterTotal(t, reg, MetricNameSamples), 0.0)
	require.True(t, hasMetricWithLabelValue(t, reg, MetricNameSuccesses, codeFwd))
	require.True(t, hasMetricWithLabelValue(t, reg, MetricNameSuccesses, codeRev))

	// Tick 3: circuits disappear → metrics for those circuits should be deleted
	atomic.StoreInt32(&present, 0)
	require.NoError(t, w.Tick(context.Background()))

	require.False(t, hasMetricWithLabelValue(t, reg, MetricNameSuccesses, codeFwd))
	require.False(t, hasMetricWithLabelValue(t, reg, MetricNameSuccesses, codeRev))
	require.False(t, hasMetricWithLabelValue(t, reg, MetricNameLosses, codeFwd))
	require.False(t, hasMetricWithLabelValue(t, reg, MetricNameLosses, codeRev))
	require.False(t, hasMetricWithLabelValue(t, reg, MetricNameSamples, codeFwd))
	require.False(t, hasMetricWithLabelValue(t, reg, MetricNameSamples, codeRev))
	require.False(t, hasMetricWithLabelValue(t, reg, MetricNameAccountNotFound, codeFwd))
	require.False(t, hasMetricWithLabelValue(t, reg, MetricNameAccountNotFound, codeRev))

	// Internal stats entries for these circuits should be scrubbed
	w.mu.RLock()
	for k := range w.stats {
		require.NotContains(t, k, "-"+codeFwd)
		require.NotContains(t, k, "-"+codeRev)
	}
	w.mu.RUnlock()
}

func TestWatcher_Tick_DeletesOnlyDisappeared_Circuit(t *testing.T) {
	t.Parallel()

	cfg, reg := baseCfg(t)

	// Two links → four circuits; later we "remove" only linkA
	oa, za := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ob, zb := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	contributorPK := solana.NewWallet().PublicKey()
	linkA, linkB := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	oaCode, zaCode := "OA", "ZA"
	obCode, zbCode := "OB", "ZB"

	codeA1 := circuitKey(oaCode, zaCode, linkA)
	codeA2 := circuitKey(zaCode, oaCode, linkA)
	codeB1 := circuitKey(obCode, zbCode, linkB)
	codeB2 := circuitKey(zbCode, obCode, linkB)

	var step atomic.Int32
	var keepA atomic.Bool
	keepA.Store(true)

	cfg.LedgerRPCClient = &mockLedgerRPC{
		GetEpochInfoFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 12}, nil
		},
	}
	cfg.Serviceability = &mockServiceabilityClient{
		GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			// both links initially; then drop linkA
			pd := &serviceability.ProgramData{
				Devices: []serviceability.Device{
					{Code: oaCode, PubKey: pkAsBytes(oa)}, {Code: zaCode, PubKey: pkAsBytes(za)},
					{Code: obCode, PubKey: pkAsBytes(ob)}, {Code: zbCode, PubKey: pkAsBytes(zb)},
				},
				Contributors: []serviceability.Contributor{
					{Code: "C1", PubKey: pkAsBytes(contributorPK)},
				},
			}
			if keepA.Load() {
				pd.Links = append(pd.Links, serviceability.Link{
					Code: "LA", PubKey: pkAsBytes(linkA), SideAPubKey: pkAsBytes(oa), SideZPubKey: pkAsBytes(za), ContributorPubKey: pkAsBytes(contributorPK),
				})
			}
			pd.Links = append(pd.Links, serviceability.Link{
				Code: "LB", PubKey: pkAsBytes(linkB), SideAPubKey: pkAsBytes(ob), SideZPubKey: pkAsBytes(zb), ContributorPubKey: pkAsBytes(contributorPK),
			})
			return pd, nil
		},
	}
	// simple telemetry: always returns at least one success so deltas are emitted
	cfg.Telemetry = &mockTelemetryProgramClient{
		GetDeviceLatencySamplesFunc: func(_ context.Context, o, t, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
			switch {
			case (o == oa && t == za) || (o == za && t == oa) || (o == ob && t == zb) || (o == zb && t == ob):
				// two samples so on the second tick we emit +1 success (+1 sample)
				if step.Load() == 0 {
					return &telemetry.DeviceLatencySamples{Samples: []uint32{1}}, nil
				}
				return &telemetry.DeviceLatencySamples{Samples: []uint32{1, 2}}, nil
			default:
				return &telemetry.DeviceLatencySamples{Samples: nil}, nil
			}
		},
	}

	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)

	// Tick 1: baseline
	require.NoError(t, w.Tick(context.Background()))
	// Tick 2: create series for both links
	step.Store(1)
	require.NoError(t, w.Tick(context.Background()))
	require.True(t, hasMetricWithLabelValue(t, reg, MetricNameSuccesses, codeA1))
	require.True(t, hasMetricWithLabelValue(t, reg, MetricNameSuccesses, codeB1))

	// Tick 3: drop linkA → only A* series should be deleted; B* should remain
	keepA.Store(false)
	require.NoError(t, w.Tick(context.Background()))

	require.False(t, hasMetricWithLabelValue(t, reg, MetricNameSuccesses, codeA1))
	require.False(t, hasMetricWithLabelValue(t, reg, MetricNameSuccesses, codeA2))
	require.True(t, hasMetricWithLabelValue(t, reg, MetricNameSuccesses, codeB1))
	require.True(t, hasMetricWithLabelValue(t, reg, MetricNameSuccesses, codeB2))

	// stats for A* should be scrubbed
	w.mu.RLock()
	for k := range w.stats {
		requireNotSuffix(t, k, "-"+codeA1)
		requireNotSuffix(t, k, "-"+codeA2)
	}
	w.mu.RUnlock()
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
	GetDeviceLatencySamplesFunc func(ctx context.Context, o, t, l solana.PublicKey, e uint64) (*telemetry.DeviceLatencySamples, error)
}

func (m *mockTelemetryProgramClient) GetDeviceLatencySamples(ctx context.Context, o, t, l solana.PublicKey, e uint64) (*telemetry.DeviceLatencySamples, error) {
	return m.GetDeviceLatencySamplesFunc(ctx, o, t, l, e)
}

func newTestLogger(t *testing.T) *slog.Logger {
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log = log.With("test", t.Name())
	return log
}

func circuitKey(origin, target string, linkPK solana.PublicKey) string {
	linkPKStr := linkPK.String()
	shortLinkPK := linkPKStr[len(linkPKStr)-7:]
	return fmt.Sprintf("%s → %s (%s)", origin, target, shortLinkPK)
}

func makeProgramData(devA, devZ string, pkA, pkZ, linkPK solana.PublicKey, linkCode string) *serviceability.ProgramData {
	contributor := serviceability.Contributor{Code: "C1", PubKey: pkAsBytes(solana.NewWallet().PublicKey())}
	return &serviceability.ProgramData{
		Devices: []serviceability.Device{
			{Code: devA, PubKey: pkAsBytes(pkA)},
			{Code: devZ, PubKey: pkAsBytes(pkZ)},
		},
		Links: []serviceability.Link{
			{
				Code:              linkCode,
				PubKey:            pkAsBytes(linkPK),
				SideAPubKey:       pkAsBytes(pkA),
				SideZPubKey:       pkAsBytes(pkZ),
				ContributorPubKey: contributor.PubKey,
			},
		},
		Contributors: []serviceability.Contributor{
			contributor,
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
	}, reg
}

func stepTelemetryMock(origin, target solana.PublicKey, step *int32) *mockTelemetryProgramClient {
	return &mockTelemetryProgramClient{
		GetDeviceLatencySamplesFunc: func(_ context.Context, o, tpk, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
			if o == origin && tpk == target {
				if atomic.LoadInt32(step) == 0 {
					return &telemetry.DeviceLatencySamples{Samples: []uint32{1, 2, 0, 5}}, nil // 3/1
				}
				return &telemetry.DeviceLatencySamples{Samples: []uint32{1, 2, 0, 5, 9}}, nil // +1 success
			}
			if o == target && tpk == origin {
				if atomic.LoadInt32(step) == 0 {
					return &telemetry.DeviceLatencySamples{Samples: []uint32{0, 0, 7}}, nil // 1/2
				}
				return &telemetry.DeviceLatencySamples{Samples: []uint32{0, 0, 7, 3, 0}}, nil // +1 success, +1 loss
			}
			return &telemetry.DeviceLatencySamples{Samples: nil}, nil
		},
	}
}

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

func hasMetricWithLabelValue(t *testing.T, g prometheus.Gatherer, metric, wantLabelValue string) bool {
	mfs, err := g.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != metric {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetValue() == wantLabelValue {
					return true
				}
			}
		}
	}
	return false
}

func requireNotSuffix(t require.TestingT, s, suf string, msgAndArgs ...any) {
	if strings.HasSuffix(s, suf) {
		require.Fail(t, fmt.Sprintf("expected %q to NOT have suffix %q", s, suf), msgAndArgs...)
	}
}
