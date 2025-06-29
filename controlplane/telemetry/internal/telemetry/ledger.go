package telemetry

import (
	"context"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

// ServiceabilityProgramClient is the client to the serviceability program.
//
// It conforms to the smartcontract/sdk/go/client.Client structure so that
// and is useful for mocking in tests.
type ServiceabilityProgramClient interface {
	// Load loads the program from the ledger.
	Load(ctx context.Context) error

	// GetDevices returns the devices in the program.
	GetDevices() []serviceability.Device

	// GetLinks returns the links in the program.
	GetLinks() []serviceability.Link
}

// TelemetryProgramClient is the client to the telemetry program.
type TelemetryProgramClient interface {
	// InitializeDeviceLatencySamples initializes the device latency samples account.
	InitializeDeviceLatencySamples(ctx context.Context, config telemetry.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error)

	// WriteDeviceLatencySamples writes the device latency samples to the account.
	WriteDeviceLatencySamples(ctx context.Context, config telemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error)
}
