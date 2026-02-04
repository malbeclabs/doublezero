package solbalance

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

type SolBalanceRPCClient interface {
	GetBalance(ctx context.Context, pubkey solana.PublicKey, commitment solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error)
}

type Config struct {
	Logger    *slog.Logger
	Interval  time.Duration
	RPCClient SolBalanceRPCClient
	Accounts  map[string]solana.PublicKey
	Threshold float64
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Interval <= 0 {
		return errors.New("interval must be greater than 0")
	}
	if c.RPCClient == nil {
		return errors.New("rpc client is required")
	}
	if len(c.Accounts) == 0 {
		return errors.New("at least one account is required")
	}
	return nil
}
