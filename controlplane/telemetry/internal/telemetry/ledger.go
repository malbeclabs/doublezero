package telemetry

import (
	"context"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

// ServiceabilityProgramClient is the client to the serviceability program.
//
// It conforms to the smartcontract/sdk/go/client.Client structure so that
// and is useful for mocking in tests.
type ServiceabilityProgramClient interface {
	// GetProgramData returns the program data from the ledger.
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

// TelemetryProgramClient is the client to the telemetry program.
type TelemetryProgramClient interface {
	// InitializeDeviceLatencySamples initializes the device latency samples account.
	InitializeDeviceLatencySamples(ctx context.Context, config telemetry.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error)

	// WriteDeviceLatencySamples writes the device latency samples to the account.
	WriteDeviceLatencySamples(ctx context.Context, config telemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error)
}
