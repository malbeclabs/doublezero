package funder

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

var (
	ErrLoggerRequired         = errors.New("logger is required")
	ErrServiceabilityRequired = errors.New("serviceability is required")
	ErrSolanaRequired         = errors.New("solana is required")
	ErrSignerRequired         = errors.New("signer is required")
	ErrSignerInvalid          = errors.New("signer is invalid")
	ErrMinBalanceRequired     = errors.New("min balance is required")
	ErrTopUpSOLRequired       = errors.New("top up is required")
	ErrIntervalRequired       = errors.New("interval is required")
)

const (
	defaultWaitForBalanceTimeout      = 60 * time.Second
	defaultWaitForBalancePollInterval = 1 * time.Second
)

type Config struct {
	Logger         *slog.Logger
	Serviceability ServiceabilityClient
	Solana         SolanaClient

	Signer                     solana.PrivateKey
	MinBalanceSOL              float64
	TopUpSOL                   float64
	Interval                   time.Duration
	WaitForBalanceTimeout      time.Duration
	WaitForBalancePollInterval time.Duration
}

func (c *Config) MinBalanceLamports() uint64 {
	return uint64(c.MinBalanceSOL * float64(solana.LAMPORTS_PER_SOL))
}

func (c *Config) TopUpLamports() uint64 {
	return uint64(c.TopUpSOL * float64(solana.LAMPORTS_PER_SOL))
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return ErrLoggerRequired
	}
	if c.Serviceability == nil {
		return ErrServiceabilityRequired
	}
	if c.Solana == nil {
		return ErrSolanaRequired
	}
	if c.Signer == nil {
		return ErrSignerRequired
	}
	if !c.Signer.IsValid() {
		return ErrSignerInvalid
	}
	if c.MinBalanceSOL <= 0.0 {
		return ErrMinBalanceRequired
	}
	if c.TopUpSOL <= 0.0 {
		return ErrTopUpSOLRequired
	}
	if c.Interval <= 0 {
		return ErrIntervalRequired
	}
	if c.WaitForBalanceTimeout <= 0 {
		c.WaitForBalanceTimeout = defaultWaitForBalanceTimeout
	}
	if c.WaitForBalancePollInterval <= 0 {
		c.WaitForBalancePollInterval = defaultWaitForBalancePollInterval
	}
	return nil
}

type ServiceabilityClient interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
	ProgramID() solana.PublicKey
}

type SolanaClient interface {
	GetBalance(context.Context, solana.PublicKey, solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error)
	GetLatestBlockhash(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error)
	SendTransactionWithOpts(context.Context, *solana.Transaction, solanarpc.TransactionOpts) (solana.Signature, error)
}
