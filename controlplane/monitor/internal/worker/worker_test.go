package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

func TestMonitor_Worker(t *testing.T) {
	t.Parallel()

	validCfg := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 1}, nil
			},
		},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{}, nil
			},
		},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(context.Context, solana.PublicKey, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.DeviceLatencySamples, error) {
				return &telemetry.DeviceLatencySamples{Samples: []uint32{}}, nil
			},
			GetInternetLatencySamplesFunc: func(ctx context.Context, d string, o, t, l solana.PublicKey, e uint64) (*telemetry.InternetLatencySamples, error) {
				return &telemetry.InternetLatencySamples{Samples: []uint32{}}, nil
			},
		},
		InternetLatencyCollectorPK: solana.NewWallet().PublicKey(),
		Interval:                   10 * time.Millisecond,
		TwoZOracleClient:           &mockTwoZOracleClient{},
		TwoZOracleInterval:         10 * time.Millisecond,
	}

	t.Run("New_setsUpDeviceTelemetryWatcher", func(t *testing.T) {
		t.Parallel()
		w, err := New(validCfg)
		require.NoError(t, err)
		require.NotNil(t, w)
		require.Len(t, w.watchers, 4)
		require.Equal(t, "serviceability", w.watchers[0].Name())
		require.Equal(t, "device-telemetry", w.watchers[1].Name())
		require.Equal(t, "internet-telemetry", w.watchers[2].Name())
		require.Equal(t, "twozoracle", w.watchers[3].Name())
	})

	t.Run("New_failsOnBadConfig", func(t *testing.T) {
		t.Parallel()
		c := *validCfg
		c.Logger = nil
		w, err := New(&c)
		require.Error(t, err)
		require.Nil(t, w)
	})

	t.Run("Run_cancelsWhenWatcherReturnsError", func(t *testing.T) {
		t.Parallel()

		errWatcher := &mockWatcher{
			NameFunc: func() string { return "err-w" },
			RunFunc: func(ctx context.Context) error {
				time.Sleep(5 * time.Millisecond)
				return errors.New("boom")
			},
		}
		w := &Worker{
			log:      newTestLogger(t),
			cfg:      validCfg,
			watchers: []Watcher{errWatcher},
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan struct{})
		go func() { _ = w.Run(ctx); close(done) }()

		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("worker did not exit after watcher error")
		}
	})

	t.Run("Run_exitsOnParentContextCancel", func(t *testing.T) {
		t.Parallel()

		blocking := &mockWatcher{
			NameFunc: func() string { return "block" },
			RunFunc: func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			},
		}
		w := &Worker{
			log:      newTestLogger(t),
			cfg:      validCfg,
			watchers: []Watcher{blocking},
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { _ = w.Run(ctx); close(done) }()

		time.Sleep(10 * time.Millisecond)
		cancel()

		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("worker did not exit after cancel")
		}
	})

	t.Run("Run_startsAllWatchers", func(t *testing.T) {
		t.Parallel()

		var started1, started2 atomic.Int32

		w1 := &mockWatcher{
			NameFunc: func() string { return "w1" },
			RunFunc: func(ctx context.Context) error {
				started1.Store(1)
				<-ctx.Done()
				return nil
			},
		}
		w2 := &mockWatcher{
			NameFunc: func() string { return "w2" },
			RunFunc: func(ctx context.Context) error {
				started2.Store(1)
				<-ctx.Done()
				return nil
			},
		}

		w := &Worker{
			log:      newTestLogger(t),
			cfg:      validCfg,
			watchers: []Watcher{w1, w2},
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { _ = w.Run(ctx); close(done) }()

		require.Eventually(t, func() bool {
			return started1.Load() == 1 && started2.Load() == 1
		}, 3*time.Second, 100*time.Millisecond)

		cancel()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("worker did not exit after cancel")
		}
	})
}

type mockWatcher struct {
	NameFunc func() string
	RunFunc  func(ctx context.Context) error
}

func (m *mockWatcher) Name() string {
	if m.NameFunc != nil {
		return m.NameFunc()
	}
	return "mock"
}
func (m *mockWatcher) Run(ctx context.Context) error {
	if m.RunFunc != nil {
		return m.RunFunc(ctx)
	}
	return nil
}
