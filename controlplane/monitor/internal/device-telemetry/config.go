package devicetelemetry

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

const (
	defaultMaxConcurrency = 16
)

type LedgerRPCClient interface {
	GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
}

type ServiceabilityClient interface {
	GetProgramData(context.Context) (*serviceability.ProgramData, error)
}

type TelemetryProgramClient interface {
	GetDeviceLatencySamples(ctx context.Context, originDevicePubKey, targetDevicePubKey, linkPubKey solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error)
}

type Config struct {
	Logger          *slog.Logger
	Metrics         *Metrics
	LedgerRPCClient LedgerRPCClient
	Serviceability  ServiceabilityClient
	Telemetry       TelemetryProgramClient
	Interval        time.Duration
	MaxConcurrency  int
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Metrics == nil {
		return errors.New("metrics is required")
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
	if c.MaxConcurrency <= 0 {
		c.MaxConcurrency = defaultMaxConcurrency
	}
	return nil
}
