package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

type LedgerRPCClient interface {
	GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
	GetBlockTime(ctx context.Context, slot uint64) (*solana.UnixTimeSeconds, error)
	GetSlot(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error)
}

type ServiceabilityClient interface {
	GetProgramData(context.Context) (*serviceability.ProgramData, error)
}

type TelemetryProgramClient interface {
	GetDeviceLatencySamples(ctx context.Context, originDevicePubKey, targetDevicePubKey, linkPubKey solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error)
}

type Config struct {
	Logger          *slog.Logger
	LedgerRPCClient LedgerRPCClient
	Serviceability  ServiceabilityClient
	Telemetry       TelemetryProgramClient
	Interval        time.Duration
	SlackWebhookURL string
	Env             string

	// Burn-in slot counts for devices/links.
	// ProvisioningSlotCount is used for new devices/links (status = Provisioning, DeviceProvisioning, LinkProvisioning).
	// DrainedSlotCount is used for reactivated devices/links (status = Drained, HardDrained, SoftDrained).
	ProvisioningSlotCount uint64
	DrainedSlotCount      uint64
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.LedgerRPCClient == nil {
		return errors.New("ledger rpc client is required")
	}
	if c.Serviceability == nil {
		return errors.New("serviceability client is required")
	}
	if c.Telemetry == nil {
		return errors.New("telemetry client is required")
	}
	if c.Interval <= 0 {
		return errors.New("interval must be greater than 0")
	}
	return nil
}
