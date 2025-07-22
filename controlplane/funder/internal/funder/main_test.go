package funder_test

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

var (
	logger *slog.Logger
)

// TestMain sets up the test environment with a global logger.
func TestMain(m *testing.M) {
	flag.Parse()
	verbose := false
	if vFlag := flag.Lookup("test.v"); vFlag != nil && vFlag.Value.String() == "true" {
		verbose = true
	}
	if verbose {
		logger = slog.New(tint.NewHandler(os.Stdout, &tint.Options{
			Level:      slog.LevelDebug,
			TimeFormat: time.RFC3339,
			AddSource:  true,
		}))
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	os.Exit(m.Run())
}

type mockServiceability struct {
	GetProgramDataFunc func(ctx context.Context) (*serviceability.ProgramData, error)
	ProgramIDFunc      func() solana.PublicKey
}

func (m *mockServiceability) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return m.GetProgramDataFunc(ctx)
}

func (m *mockServiceability) ProgramID() solana.PublicKey {
	return m.ProgramIDFunc()
}

type mockSolana struct {
	GetBalanceFunc              func(ctx context.Context, pubkey solana.PublicKey, commitment solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error)
	GetLatestBlockhashFunc      func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error)
	SendTransactionWithOptsFunc func(ctx context.Context, tx *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error)
}

func (m *mockSolana) GetBalance(ctx context.Context, pubkey solana.PublicKey, commitment solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
	return m.GetBalanceFunc(ctx, pubkey, commitment)
}

func (m *mockSolana) GetLatestBlockhash(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
	return m.GetLatestBlockhashFunc(ctx, commitment)
}

func (m *mockSolana) SendTransactionWithOpts(ctx context.Context, tx *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
	return m.SendTransactionWithOptsFunc(ctx, tx, opts)
}
