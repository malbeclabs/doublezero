package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	twozoracle "github.com/malbeclabs/doublezero/controlplane/monitor/internal/2z-oracle"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

type LedgerRPCClient interface {
	GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
}

type ServiceabilityClient interface {
	GetProgramData(context.Context) (*serviceability.ProgramData, error)
}

type TelemetryProgramClient interface {
	GetDeviceLatencySamples(ctx context.Context, originDevicePubKey, targetDevicePubKey, linkPubKey solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error)
	GetInternetLatencySamples(ctx context.Context, dataProviderName string, originExchangePubKey, targetExchangePubKey, linkPubKey solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error)
}

type InfluxWriter interface {
	Errors() <-chan error
	WriteRecord(string)
	Flush()
}

type SolBalanceRPCClient interface {
	GetBalance(ctx context.Context, pubkey solana.PublicKey, commitment solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error)
}

type Config struct {
	Logger                     *slog.Logger
	LedgerRPCClient            LedgerRPCClient
	SolanaRPCClient            LedgerRPCClient
	Serviceability             ServiceabilityClient
	Telemetry                  TelemetryProgramClient
	InternetLatencyCollectorPK solana.PublicKey
	Interval                   time.Duration
	SlackWebhookURL            string
	TwoZOracleClient           twozoracle.TwoZOracleClient
	TwoZOracleInterval         time.Duration
	InfluxWriter               InfluxWriter
	Env                        string
	SolBalanceRPCClient        SolBalanceRPCClient
	SolBalanceAccounts         map[string]solana.PublicKey
	SolBalanceThreshold        float64
	SolBalanceInterval         time.Duration
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
		return errors.New("internet latency collector pk is required")
	}
	if c.Interval <= 0 {
		return errors.New("interval must be greater than 0")
	}
	if c.TwoZOracleInterval <= 0 {
		return errors.New("twoz oracle interval must be greater than 0")
	}
	return nil
}
