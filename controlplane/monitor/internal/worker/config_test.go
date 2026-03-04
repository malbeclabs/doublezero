package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	twozoracle "github.com/malbeclabs/doublezero/controlplane/monitor/internal/2z-oracle"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

func TestMonitor_Worker_Config(t *testing.T) {
	t.Parallel()

	valid := &Config{
		Logger: newTestLogger(t),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 1}, nil
			},
		},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{}, nil
			},
		},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, o, t, l solana.PublicKey, e uint64) (*telemetry.DeviceLatencySamples, error) {
				return &telemetry.DeviceLatencySamples{}, nil
			},
			GetInternetLatencySamplesFunc: func(ctx context.Context, d string, o, t, l solana.PublicKey, e uint64) (*telemetry.InternetLatencySamples, error) {
				return &telemetry.InternetLatencySamples{}, nil
			},
		},
		Interval:                   50 * time.Millisecond,
		InternetLatencyCollectorPK: solana.NewWallet().PublicKey(),
		TwoZOracleClient:           &mockTwoZOracleClient{},
		TwoZOracleInterval:         50 * time.Millisecond,
		InfluxWriter: &mockInfluxWriter{
			ErrorsFunc: func() <-chan error {
				errCh := make(chan error)
				close(errCh)
				return errCh
			},
			WriteFunc: func(s string) {},
			FlushFunc: func() {},
		},
	}

	t.Run("valid config passes", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, valid.Validate())
	})

	t.Run("missing logger fails", func(t *testing.T) {
		t.Parallel()
		c := *valid
		c.Logger = nil
		require.Error(t, c.Validate())
	})

	t.Run("missing ledger rpc fails", func(t *testing.T) {
		t.Parallel()
		c := *valid
		c.LedgerRPCClient = nil
		require.Error(t, c.Validate())
	})

	t.Run("missing serviceability fails", func(t *testing.T) {
		t.Parallel()
		c := *valid
		c.Serviceability = nil
		require.Error(t, c.Validate())
	})

	t.Run("missing telemetry fails", func(t *testing.T) {
		t.Parallel()
		c := *valid
		c.Telemetry = nil
		require.Error(t, c.Validate())
	})

	t.Run("missing internet latency collector pk fails", func(t *testing.T) {
		t.Parallel()
		c := *valid
		c.InternetLatencyCollectorPK = solana.PublicKey{}
		require.Error(t, c.Validate())
	})

	t.Run("non-positive interval fails", func(t *testing.T) {
		t.Parallel()
		c := *valid
		c.Interval = 0
		require.Error(t, c.Validate())
	})

	t.Run("twoz oracle can be nil", func(t *testing.T) {
		t.Parallel()
		c := *valid
		c.TwoZOracleClient = nil
		require.NoError(t, c.Validate())
	})

	t.Run("non-positive twoz oracle interval fails", func(t *testing.T) {
		t.Parallel()
		c := *valid
		c.TwoZOracleInterval = 0
		require.Error(t, c.Validate())
	})
}

func newTestLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})).With("test", t.Name())
}

type mockLedgerRPC struct {
	GetEpochInfoFunc func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
}

func (m *mockLedgerRPC) GetEpochInfo(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
	return m.GetEpochInfoFunc(ctx, c)
}

type mockServiceabilityClient struct {
	GetProgramDataFunc                              func(context.Context) (*serviceability.ProgramData, error)
	GetMulticastPublisherBlockResourceExtensionFunc func(context.Context) (*serviceability.ResourceExtension, error)
}

func (m *mockServiceabilityClient) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return m.GetProgramDataFunc(ctx)
}

func (m *mockServiceabilityClient) GetMulticastPublisherBlockResourceExtension(ctx context.Context) (*serviceability.ResourceExtension, error) {
	if m.GetMulticastPublisherBlockResourceExtensionFunc != nil {
		return m.GetMulticastPublisherBlockResourceExtensionFunc(ctx)
	}
	return nil, nil
}

type mockTelemetryProgramClient struct {
	GetDeviceLatencySamplesFunc   func(context.Context, solana.PublicKey, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.DeviceLatencySamples, error)
	GetInternetLatencySamplesFunc func(context.Context, string, solana.PublicKey, solana.PublicKey, solana.PublicKey, uint64) (*telemetry.InternetLatencySamples, error)
}

func (m *mockTelemetryProgramClient) GetDeviceLatencySamples(ctx context.Context, o, t, l solana.PublicKey, e uint64) (*telemetry.DeviceLatencySamples, error) {
	return m.GetDeviceLatencySamplesFunc(ctx, o, t, l, e)
}

func (m *mockTelemetryProgramClient) GetInternetLatencySamples(ctx context.Context, d string, o, t, l solana.PublicKey, e uint64) (*telemetry.InternetLatencySamples, error) {
	return m.GetInternetLatencySamplesFunc(ctx, d, o, t, l, e)
}

type mockTwoZOracleClient struct {
	SwapRateFunc func(ctx context.Context) (twozoracle.SwapRateResponse, int, error)
	HealthFunc   func(ctx context.Context) (twozoracle.HealthResponse, int, error)
}

func (m *mockTwoZOracleClient) SwapRate(ctx context.Context) (twozoracle.SwapRateResponse, int, error) {
	return m.SwapRateFunc(ctx)
}

func (m *mockTwoZOracleClient) Health(ctx context.Context) (twozoracle.HealthResponse, int, error) {
	return m.HealthFunc(ctx)
}

type mockInfluxWriter struct {
	ErrorsFunc func() <-chan error
	WriteFunc  func(string)
	FlushFunc  func()
}

func (m *mockInfluxWriter) Errors() <-chan error {
	return m.ErrorsFunc()
}

func (m *mockInfluxWriter) WriteRecord(s string) {
	m.WriteFunc(s)
}

func (m *mockInfluxWriter) Flush() {
	m.FlushFunc()
}
