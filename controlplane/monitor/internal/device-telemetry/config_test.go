package devicetelemetry

import (
	"context"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

func TestMonitor_DeviceTelemetry_Config(t *testing.T) {
	t.Parallel()

	valid := &Config{
		Logger:  newTestLogger(t),
		Metrics: NewMetrics(),
		LedgerRPCClient: &mockLedgerRPC{
			GetEpochInfoFunc: func(ctx context.Context, c solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{Epoch: 1}, nil
			}},
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{}, nil
			}},
		Telemetry: &mockTelemetryProgramClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, _, _, _ solana.PublicKey, _ uint64) (*telemetry.DeviceLatencySamples, error) {
				return &telemetry.DeviceLatencySamples{}, nil
			}},
		Interval: 50 * time.Millisecond,
	}

	t.Run("valid config passes", func(t *testing.T) {
		require.NoError(t, valid.Validate())
	})

	t.Run("missing logger fails", func(t *testing.T) {
		c := *valid
		c.Logger = nil
		require.Error(t, c.Validate())
	})

	t.Run("missing metrics fails", func(t *testing.T) {
		c := *valid
		c.Metrics = nil
		require.Error(t, c.Validate())
	})

	t.Run("missing ledger RPC fails", func(t *testing.T) {
		c := *valid
		c.LedgerRPCClient = nil
		require.Error(t, c.Validate())
	})

	t.Run("missing serviceability fails", func(t *testing.T) {
		c := *valid
		c.Serviceability = nil
		require.Error(t, c.Validate())
	})

	t.Run("missing telemetry fails", func(t *testing.T) {
		c := *valid
		c.Telemetry = nil
		require.Error(t, c.Validate())
	})

	t.Run("missing interval fails", func(t *testing.T) {
		c := *valid
		c.Interval = 0
		require.Error(t, c.Validate())
	})
}
