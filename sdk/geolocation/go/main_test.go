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
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
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

	GetAccountInfoFunc             func(context.Context, solana.PublicKey) (*solanarpc.GetAccountInfoResult, error)
	GetProgramAccountsWithOptsFunc func(context.Context, solana.PublicKey, *solanarpc.GetProgramAccountsOpts) (solanarpc.GetProgramAccountsResult, error)
}

func (m *mockRPCClient) GetAccountInfo(ctx context.Context, account solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
	return m.GetAccountInfoFunc(ctx, account)
}

func (m *mockRPCClient) GetProgramAccountsWithOpts(ctx context.Context, publicKey solana.PublicKey, opts *solanarpc.GetProgramAccountsOpts) (solanarpc.GetProgramAccountsResult, error) {
	return m.GetProgramAccountsWithOptsFunc(ctx, publicKey, opts)
}
