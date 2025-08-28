package internettelemetry

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
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
	GetInternetLatencySamples(ctx context.Context, dataProviderName string, originExchangePK, targetExchangePK, agentPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error)
}

type Config struct {
	Logger                     *slog.Logger
	LedgerRPCClient            LedgerRPCClient
	Serviceability             ServiceabilityClient
	InternetLatencyCollectorPK solana.PublicKey
	Telemetry                  TelemetryProgramClient
	Interval                   time.Duration
	MaxConcurrency             int
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
	if c.InternetLatencyCollectorPK.IsZero() {
		return errors.New("internet latency collector public key is required")
	}
	if c.Interval <= 0 {
		return errors.New("interval must be greater than 0")
	}
	if c.MaxConcurrency <= 0 {
		c.MaxConcurrency = defaultMaxConcurrency
	}
	return nil
}
