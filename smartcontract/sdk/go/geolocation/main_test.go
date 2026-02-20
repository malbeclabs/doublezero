package geolocation_test

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/geolocation"
)

var (
	log *slog.Logger
)

// TestMain sets up the test environment with a global logger.
func TestMain(m *testing.M) {
	flag.Parse()
	verbose := false
	if vFlag := flag.Lookup("test.v"); vFlag != nil && vFlag.Value.String() == "true" {
		verbose = true
	}
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	log = slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      logLevel,
		TimeFormat: time.RFC3339,
		AddSource:  true,
	}))

	os.Exit(m.Run())
}

type mockRPCClient struct {
	geolocation.RPCClient

	GetLatestBlockhashFunc         func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error)
	SendTransactionWithOptsFunc    func(context.Context, *solana.Transaction, solanarpc.TransactionOpts) (solana.Signature, error)
	GetSignatureStatusesFunc       func(context.Context, bool, ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error)
	GetTransactionFunc             func(context.Context, solana.Signature, *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error)
	GetAccountInfoFunc             func(context.Context, solana.PublicKey) (*solanarpc.GetAccountInfoResult, error)
	GetProgramAccountsWithOptsFunc func(context.Context, solana.PublicKey, *solanarpc.GetProgramAccountsOpts) (solanarpc.GetProgramAccountsResult, error)
}

func (m *mockRPCClient) GetLatestBlockhash(ctx context.Context, ct solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
	return m.GetLatestBlockhashFunc(ctx, ct)
}

func (m *mockRPCClient) SendTransactionWithOpts(ctx context.Context, tx *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
	return m.SendTransactionWithOptsFunc(ctx, tx, opts)
}

func (m *mockRPCClient) GetSignatureStatuses(ctx context.Context, search bool, sigs ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
	return m.GetSignatureStatusesFunc(ctx, search, sigs...)
}

func (m *mockRPCClient) GetTransaction(ctx context.Context, sig solana.Signature, opts *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
	return m.GetTransactionFunc(ctx, sig, opts)
}

func (m *mockRPCClient) GetAccountInfo(ctx context.Context, account solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
	return m.GetAccountInfoFunc(ctx, account)
}

func (m *mockRPCClient) GetProgramAccountsWithOpts(ctx context.Context, publicKey solana.PublicKey, opts *solanarpc.GetProgramAccountsOpts) (solanarpc.GetProgramAccountsResult, error) {
	return m.GetProgramAccountsWithOptsFunc(ctx, publicKey, opts)
}
