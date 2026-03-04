package serviceability

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

type ServiceabilityClient interface {
	GetProgramData(context.Context) (*serviceability.ProgramData, error)
	GetMulticastPublisherBlockResourceExtension(context.Context) (*serviceability.ResourceExtension, error)
}

type LedgerRPCClient interface {
	GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
}

type InfluxWriter interface {
	Errors() <-chan error
	WriteRecord(string)
	Flush()
}

type Config struct {
	Logger          *slog.Logger
	Serviceability  ServiceabilityClient
	Interval        time.Duration
	SlackWebhookURL string
	InfluxWriter    InfluxWriter
	Env             string
	LedgerRPCClient LedgerRPCClient
	SolanaRPCClient LedgerRPCClient
	AllowOwnUsers   bool
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Serviceability == nil {
		return errors.New("serviceability client is required")
	}
	if c.Interval <= 0 {
		return errors.New("interval must be greater than 0")
	}
	return nil
}
