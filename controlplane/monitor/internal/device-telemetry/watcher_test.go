package devicetelemetry

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
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

func TestMonitor_DeviceTelemetry_Watcher_NewAndName(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 1}, nil
			}},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) { return &serviceability.ProgramData{}, nil }},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
				return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
			}},
		Interval: 10 * time.Millisecond,
	}

	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.NotNil(t, w)
	require.Equal(t, watcherName, w.Name())
}

func TestMonitor_DeviceTelemetry_Watcher_RunStopsOnCancel(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 1}, nil
			}},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) { return &serviceability.ProgramData{}, nil }},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
				return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
			}},
		Interval: 5 * time.Millisecond,
	}
	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not stop after cancel")
	}
}

func TestMonitor_DeviceTelemetry_Watcher_Tick_NoCircuits(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 9}, nil
			}},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) { return &serviceability.ProgramData{}, nil }},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
				return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
			}},
		Interval: 10 * time.Millisecond,
	}
	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)

	require.NoError(t, w.Tick(context.Background()))
	w.mu.RLock()
	defer w.mu.RUnlock()
	require.False(t, w.epochSet)
	require.Equal(t, uint64(0), w.lastEpoch)
	require.Empty(t, w.stats)
}

func TestMonitor_DeviceTelemetry_Watcher_Tick_ErrorFromGetProgramData(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 9}, nil
			}},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) { return nil, errors.New("boom") }},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
				return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
			}},
		Interval: 10 * time.Millisecond,
	}
	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.Error(t, w.Tick(context.Background()))
}

func TestMonitor_DeviceTelemetry_Watcher_Tick_ErrorFromGetEpochInfo(t *testing.T) {
	t.Parallel()

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()

	cfg := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return nil, errors.New("epoch fail")
			}},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
				return makeProgramData("OR-A", "TG-A", origin, target, link, "LK-A"), nil
			}},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
				return &telemetry.DeviceLatencySamples{Samples: []uint32{1}}, nil
			}},
		Interval: 10 * time.Millisecond,
	}
	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.Error(t, w.Tick(context.Background()))
}

func TestMonitor_DeviceTelemetry_Watcher_Tick_ErrorFromGetDeviceLatencySamples(t *testing.T) {
	t.Parallel()

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()

	cfg := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 10}, nil
			}},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
				return makeProgramData("OR-A", "TG-A", origin, target, link, "LK-A"), nil
			}},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
				return nil, errors.New("telemetry fail")
			}},
		Interval: 10 * time.Millisecond,
	}
	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.Error(t, w.Tick(context.Background()))
}

func TestMonitor_DeviceTelemetry_Watcher_Tick_SameEpoch_BaselineThenUpdate(t *testing.T) {
	t.Parallel()

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()
	originCode, targetCode := "OR-A", "TG-A"

	// local step only toggled BETWEEN ticks (never during a tick)
	step := 0

	cfg := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 10}, nil
			}},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
				return makeProgramData(originCode, targetCode, origin, target, link, "LK-A"), nil
			}},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, o, t, l solana.PublicKey, e uint64) (*telemetry.DeviceLatencySamples, error) {
				if o == origin && t == target {
					if step == 0 {
						return &telemetry.DeviceLatencySamples{Samples: []uint32{1, 2, 0, 5}}, nil
					} // 3/1
					return &telemetry.DeviceLatencySamples{Samples: []uint32{1, 2, 0, 5, 9}}, nil // 4/1
				}
				if step == 0 {
					return &telemetry.DeviceLatencySamples{Samples: []uint32{0, 0, 7}}, nil
				} // 1/2
				return &telemetry.DeviceLatencySamples{Samples: []uint32{0, 0, 7, 3, 0}}, nil // 2/3
			}},
		Interval: 10 * time.Millisecond,
	}
	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)

	ctx := context.Background()
	keyFwd := "10-" + circuitKey(originCode, targetCode, link)
	keyRev := "10-" + circuitKey(targetCode, originCode, link)

	require.NoError(t, w.Tick(ctx))
	w.mu.RLock()
	require.Equal(t, uint64(10), w.lastEpoch)
	require.True(t, w.epochSet)
	require.Equal(t, CircuitTelemetryStats{SuccessCount: 3, LossCount: 1}, w.stats[keyFwd])
	require.Equal(t, CircuitTelemetryStats{SuccessCount: 1, LossCount: 2}, w.stats[keyRev])
	w.mu.RUnlock()

	step = 1
	require.NoError(t, w.Tick(ctx))
	w.mu.RLock()
	require.Equal(t, CircuitTelemetryStats{SuccessCount: 4, LossCount: 1}, w.stats[keyFwd])
	require.Equal(t, CircuitTelemetryStats{SuccessCount: 2, LossCount: 3}, w.stats[keyRev])
	w.mu.RUnlock()
}

func TestMonitor_DeviceTelemetry_Watcher_Tick_EpochRollover(t *testing.T) {
	t.Parallel()

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()
	originCode, targetCode := "OR-A", "TG-A"

	epochVal := uint64(10) // changed only between ticks

	cfg := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: epochVal}, nil
			}},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
				return makeProgramData(originCode, targetCode, origin, target, link, "LK-A"), nil
			}},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, o, t, l solana.PublicKey, e uint64) (*telemetry.DeviceLatencySamples, error) {
				if e == 10 {
					if o == origin && t == target {
						return &telemetry.DeviceLatencySamples{Samples: []uint32{1, 2, 0, 5}}, nil
					} // 3/1
					return &telemetry.DeviceLatencySamples{Samples: []uint32{0, 0, 7}}, nil // 1/2
				}
				if o == origin && t == target {
					return &telemetry.DeviceLatencySamples{Samples: []uint32{8, 8, 0}}, nil
				} // 2/1
				return &telemetry.DeviceLatencySamples{Samples: []uint32{0}}, nil // 0/1
			}},
		Interval: 10 * time.Millisecond,
	}
	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)

	ctx := context.Background()
	key10F := "10-" + circuitKey(originCode, targetCode, link)
	key10R := "10-" + circuitKey(targetCode, originCode, link)
	key11F := "11-" + circuitKey(originCode, targetCode, link)
	key11R := "11-" + circuitKey(targetCode, originCode, link)

	require.NoError(t, w.Tick(ctx))
	w.mu.RLock()
	require.Equal(t, uint64(10), w.lastEpoch)
	require.True(t, w.epochSet)
	require.Equal(t, CircuitTelemetryStats{SuccessCount: 3, LossCount: 1}, w.stats[key10F])
	require.Equal(t, CircuitTelemetryStats{SuccessCount: 1, LossCount: 2}, w.stats[key10R])
	w.mu.RUnlock()

	epochVal = 11
	require.NoError(t, w.Tick(ctx))
	w.mu.RLock()
	require.Equal(t, uint64(11), w.lastEpoch)
	require.True(t, w.epochSet)
	require.Equal(t, CircuitTelemetryStats{SuccessCount: 2, LossCount: 1}, w.stats[key11F])
	require.Equal(t, CircuitTelemetryStats{SuccessCount: 0, LossCount: 1}, w.stats[key11R])
	// old totals remain
	require.Equal(t, CircuitTelemetryStats{SuccessCount: 3, LossCount: 1}, w.stats[key10F])
	require.Equal(t, CircuitTelemetryStats{SuccessCount: 1, LossCount: 2}, w.stats[key10R])
	w.mu.RUnlock()
}

func TestMonitor_DeviceTelemetry_Watcher_Tick_MixedCircuits_ErrorBubbles(t *testing.T) {
	t.Parallel()

	a := solana.NewWallet().PublicKey()
	b := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()

	cfg := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 42}, nil
			}},
		Serviceability: &mockServiceabilityClient{
			// one link → two circuits (A→B and B→A)
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
				return makeProgramData("A", "B", a, b, link, "L"), nil
			}},
		Telemetry: &mockTelemetryProgramClient{
			// succeed for one direction, fail for the other
			GetDeviceLatencySamplesFunc: func(ctx context.Context, o, t, l solana.PublicKey, e uint64) (*telemetry.DeviceLatencySamples, error) {
				if o == a && t == b {
					return &telemetry.DeviceLatencySamples{Samples: []uint32{1, 2, 3}}, nil
				}
				return nil, errors.New("reflector timeout")
			}},
		Interval: 10 * time.Millisecond,
	}
	w, err := NewDeviceTelemetryWatcher(cfg)
	require.NoError(t, err)
	require.Error(t, w.Tick(context.Background()))
}

func TestMonitor_DeviceTelemetry_Watcher_Run_ContinuesAfterTickError(t *testing.T) {
	t.Parallel()

	var step atomic.Int32 // 0=failing, 1=success

	cfg := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, ct solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 777}, nil
			}},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
				if step.Load() == 0 {
					return nil, errors.New("boom")
				}
				return &serviceability.ProgramData{}, nil
			}},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(context.Context, solana.PublicKey, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.DeviceLatencySamples, error) {
				return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
			}},
		Interval: 5 * time.Millisecond,
	}

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

func TestMonitor_DeviceTelemetry_Watcher_Tick_EmptySamples_WritesZeroStatsAndSetsEpoch(t *testing.T) {
	t.Parallel()

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()
	originCode, targetCode := "OR-Z", "TG-Z"

	cfg := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, ct solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 77}, nil
			}},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
				return makeProgramData(originCode, targetCode, origin, target, link, "LK-Z"), nil
			}},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(context.Context, solana.PublicKey, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.DeviceLatencySamples, error) {
				return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
			}},
		Interval: 10 * time.Millisecond,
	}
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
	return &serviceability.ProgramData{
		Devices: []serviceability.Device{
			{Code: devA, PubKey: pkAsBytes(pkA)},
			{Code: devZ, PubKey: pkAsBytes(pkZ)},
		},
		Links: []serviceability.Link{
			{
				Code:        linkCode,
				PubKey:      pkAsBytes(linkPK),
				SideAPubKey: pkAsBytes(pkA),
				SideZPubKey: pkAsBytes(pkZ),
			},
		},
	}
}

func pkAsBytes(pk solana.PublicKey) (out [32]byte) {
	copy(out[:], pk[:])
	return
}
