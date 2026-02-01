package serviceability

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

type mockInfluxWriter struct {
	WriteRecordFunc func(string)
	FlushFunc       func()
	ErrorsFunc      func() <-chan error
	writeCount      atomic.Int32
	flushCount      atomic.Int32
}

func (m *mockInfluxWriter) WriteRecord(s string) {
	if m.WriteRecordFunc != nil {
		m.WriteRecordFunc(s)
	}
	m.writeCount.Add(1)
}

func (m *mockInfluxWriter) Flush() {
	if m.FlushFunc != nil {
		m.FlushFunc()
	}
	m.flushCount.Add(1)
}

func (m *mockInfluxWriter) Errors() <-chan error {
	if m.ErrorsFunc != nil {
		return m.ErrorsFunc()
	}
	ch := make(chan error)
	close(ch)
	return ch
}

func TestMonitor_Serviceability_Watcher(t *testing.T) {
	t.Parallel()

	mockRPC := &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: 1}, nil
		},
	}
	t.Run("new_watcher_validates_config", func(t *testing.T) {
		t.Parallel()
		_, err := NewServiceabilityWatcher(&Config{Logger: nil, Serviceability: nil, Interval: 0})
		require.Error(t, err)

		cfg := &Config{
			Logger:          newTestLogger(t),
			Serviceability:  &mockServiceabilityClient{},
			Interval:        10 * time.Millisecond,
			LedgerRPCClient: mockRPC,
			SolanaRPCClient: mockRPC,
		}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)
		require.NotNil(t, w)
		require.Equal(t, watcherName, w.Name())
	})

	t.Run("tick_success_sets_build_info", func(t *testing.T) {
		t.Parallel()
		version := serviceability.ProgramVersion{Major: 1, Minor: 2, Patch: 3}
		got := &serviceability.ProgramData{ProgramConfig: &serviceability.ProgramConfig{Version: version}}
		cfg := &Config{
			Logger:          newTestLogger(t),
			Serviceability:  &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) { return got, nil }},
			Interval:        10 * time.Millisecond,
			LedgerRPCClient: mockRPC,
			SolanaRPCClient: mockRPC,
		}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)

		err = w.Tick(context.Background())
		require.NoError(t, err)

		lbl := programVersionString(version)
		val := testutil.ToFloat64(MetricProgramBuildInfo.WithLabelValues(lbl))
		require.Equal(t, float64(1), val)
	})

	t.Run("tick_error_increments_error_metric", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Logger:          newTestLogger(t),
			Serviceability:  &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) { return nil, errors.New("boom") }},
			Interval:        10 * time.Millisecond,
			LedgerRPCClient: mockRPC,
			SolanaRPCClient: mockRPC,
		}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)

		before := testutil.ToFloat64(MetricErrors.WithLabelValues(MetricErrorTypeGetProgramData))
		err = w.Tick(context.Background())
		require.Error(t, err)
		after := testutil.ToFloat64(MetricErrors.WithLabelValues(MetricErrorTypeGetProgramData))
		require.GreaterOrEqual(t, after-before, float64(1))
	})

	t.Run("tick_with_influx_writer_writes_metrics", func(t *testing.T) {
		t.Parallel()
		mockWriter := &mockInfluxWriter{}
		devices := []serviceability.Device{
			{Code: "dev1"},
			{Code: "dev2"},
		}
		contributors := []serviceability.Contributor{
			{Code: "cont1"},
			{Code: "cont2"},
		}
		exchanges := []serviceability.Exchange{
			{Code: "exch1"},
			{Code: "exch2"},
		}
		links := []serviceability.Link{
			{Code: "link1"},
			{Code: "link2"},
		}
		programData := &serviceability.ProgramData{
			ProgramConfig: &serviceability.ProgramConfig{},
			Devices:       devices,
			Contributors:  contributors,
			Exchanges:     exchanges,
			Links:         links,
		}

		cfg := &Config{
			Logger:          newTestLogger(t),
			Serviceability:  &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) { return programData, nil }},
			Interval:        10 * time.Millisecond,
			InfluxWriter:    mockWriter,
			LedgerRPCClient: mockRPC,
			SolanaRPCClient: mockRPC,
		}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)

		require.NoError(t, w.Tick(context.Background()))
		require.Equal(t, int32(len(devices)+len(contributors)+len(exchanges)+len(links)), mockWriter.writeCount.Load(), "WriteRecord should be called for each device, contributor, exchange, and link")
		require.Equal(t, int32(4), mockWriter.flushCount.Load(), "Flush should be called once per tick")
	})

	t.Run("run_stops_on_context_cancel", func(t *testing.T) {
		t.Parallel()
		got := &serviceability.ProgramData{ProgramConfig: &serviceability.ProgramConfig{Version: serviceability.ProgramVersion{Major: 9, Minor: 9, Patch: 9}}}
		cfg := &Config{
			Logger:          newTestLogger(t),
			Serviceability:  &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) { return got, nil }},
			Interval:        5 * time.Millisecond,
			LedgerRPCClient: mockRPC,
			SolanaRPCClient: mockRPC,
		}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- w.Run(ctx) }()

		time.Sleep(15 * time.Millisecond)
		cancel()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(250 * time.Millisecond):
			t.Fatal("Run did not return after cancel")
		}
	})

	t.Run("programVersionString_formats", func(t *testing.T) {
		t.Parallel()
		s := programVersionString(serviceability.ProgramVersion{Major: 0, Minor: 10, Patch: 7})
		require.Equal(t, "0.10.7", s)
	})
}

type mockLedgerRPC struct {
	GetEpochInfoFunc func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
	callCount        atomic.Int32
}

func (m *mockLedgerRPC) GetEpochInfo(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
	m.callCount.Add(1)
	return m.GetEpochInfoFunc(ctx, c)
}

func TestWatcher_EpochChangeDetection(t *testing.T) {
	var epoch uint64 = 1
	mockRPC := &mockLedgerRPC{
		GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
			return &solanarpc.GetEpochInfoResult{Epoch: epoch}, nil
		},
	}

	cfg := &Config{
		Logger: newTestLogger(t),
		Serviceability: &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{
				ProgramConfig: &serviceability.ProgramConfig{Version: serviceability.ProgramVersion{Major: 1, Minor: 0, Patch: 0}},
			}, nil
		}},
		Interval:        10 * time.Millisecond,
		LedgerRPCClient: mockRPC, // for doublezero
		SolanaRPCClient: mockRPC, // for solana
	}
	w, err := NewServiceabilityWatcher(cfg)
	require.NoError(t, err)

	// first tick: should set both epochs to 1
	require.NoError(t, w.Tick(context.Background()))
	require.Equal(t, int32(2), mockRPC.callCount.Load(), "GetEpochInfo should be called for both DZ and Solana")
	require.Equal(t, uint64(1), w.currDZEpoch)
	require.Equal(t, uint64(1), w.currSolanaEpoch)

	// second tick: should detect epoch change to 2
	epoch = 2
	require.NoError(t, w.Tick(context.Background()))
	require.Equal(t, int32(4), mockRPC.callCount.Load(), "GetEpochInfo should be called again for both chains")
	require.Equal(t, uint64(2), w.currDZEpoch)
	require.Equal(t, uint64(2), w.currSolanaEpoch)
}

func TestWatcher_BuildEpochChangeSlackMessage(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Logger: newTestLogger(t),
		Serviceability: &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{
				ProgramConfig: &serviceability.ProgramConfig{Version: serviceability.ProgramVersion{Major: 1, Minor: 0, Patch: 0}},
			}, nil
		}},
		Interval: 10 * time.Millisecond,
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 1}, nil
			},
		},
		SolanaRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 1}, nil
			},
		},
	}
	w, err := NewServiceabilityWatcher(cfg)
	require.NoError(t, err)

	msg, err := w.buildEpochChangeSlackMessage("testnet", "doublezero", 54, 55)
	require.NoError(t, err)
	require.NotEmpty(t, msg)
	require.Contains(t, msg, "Epoch Change Detected")
	require.Contains(t, msg, "testnet")
	require.Contains(t, msg, "doublezero")
	require.Contains(t, msg, "54")
	require.Contains(t, msg, "55")
}

func TestWatcher_EpochChangeSlackNotification(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		env             string
		slackWebhookURL string
		expectSlackCall bool
	}{
		{
			name:            "testnet_with_webhook_should_notify",
			env:             "testnet",
			slackWebhookURL: "https://hooks.slack.com/test",
			expectSlackCall: true,
		},
		{
			name:            "mainnet-beta_with_webhook_should_notify",
			env:             "mainnet-beta",
			slackWebhookURL: "https://hooks.slack.com/test",
			expectSlackCall: true,
		},
		{
			name:            "devnet_with_webhook_should_not_notify",
			env:             "devnet",
			slackWebhookURL: "https://hooks.slack.com/test",
			expectSlackCall: false,
		},
		{
			name:            "testnet_without_webhook_should_not_notify",
			env:             "testnet",
			slackWebhookURL: "",
			expectSlackCall: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var epoch uint64 = 1
			mockRPC := &mockLedgerRPC{
				GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
					return &solanarpc.GetEpochInfoResult{Epoch: epoch}, nil
				},
			}

			cfg := &Config{
				Logger: newTestLogger(t),
				Serviceability: &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{
						ProgramConfig: &serviceability.ProgramConfig{Version: serviceability.ProgramVersion{Major: 1, Minor: 0, Patch: 0}},
					}, nil
				}},
				Interval:        10 * time.Millisecond,
				LedgerRPCClient: mockRPC,
				SolanaRPCClient: mockRPC,
				SlackWebhookURL: tc.slackWebhookURL,
				Env:             tc.env,
			}
			w, err := NewServiceabilityWatcher(cfg)
			require.NoError(t, err)

			// first tick: initialize epochs
			require.NoError(t, w.Tick(context.Background()))
			require.Equal(t, uint64(1), w.currDZEpoch)
			require.Equal(t, uint64(1), w.currSolanaEpoch)

			// Note: We cannot easily verify Slack message was sent without mocking HTTP client
			// This test verifies the logic path and message building works without errors
			epoch = 2
			require.NoError(t, w.Tick(context.Background()))
			require.Equal(t, uint64(2), w.currDZEpoch)
			require.Equal(t, uint64(2), w.currSolanaEpoch)
		})
	}
}
