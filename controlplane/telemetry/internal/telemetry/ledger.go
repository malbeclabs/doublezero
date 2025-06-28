package telemetry

import (
	"context"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
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
	// AddSamples adds telemetry samples to the program.
	AddSamples(ctx context.Context, samples []Sample) error
}
