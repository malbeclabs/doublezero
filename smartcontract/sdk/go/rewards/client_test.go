package rewards_test

import (
	"context"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/rewards"
)

func TestRewardsClient(t *testing.T) {
	// TODO: Implement test
	// rpcEndpoint := "https://api.testnet.solana.com"
	rpcEndpoint := config.TestnetLedgerPublicRPCURL
	programID := solana.MustPublicKeyFromBase58("dzrecxigtaZQ3gPmt2X5mDkYigaruFR1rHCqztFTvx7")
	rpc := rpc.New(rpcEndpoint)

	client := rewards.New(rpc, programID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stats, err := client.GetTelemetryStats(ctx)
	if err != nil {
		t.Fatalf("failed to get program data: %v", err)
	}
	t.Logf("Telemetry Stats: %+v", stats)

}
