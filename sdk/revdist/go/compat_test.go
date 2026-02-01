package revdist

import (
	"context"
	"encoding/binary"
	"os"
	"testing"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
)

// These tests fetch live mainnet data and verify that our struct deserialization
// matches raw byte reads at known offsets. Run with:
//
//	go test -tags compat -run TestCompat -v ./sdk/revdist/go/
//
// Requires network access to Solana mainnet RPC.

func skipUnlessCompat(t *testing.T) {
	t.Helper()
	if os.Getenv("REVDIST_COMPAT_TEST") == "" {
		t.Skip("set REVDIST_COMPAT_TEST=1 to run compatibility tests against mainnet")
	}
}

// rpcLedgerClient implements LedgerRecordClient using a Solana RPC endpoint.
type rpcLedgerClient struct {
	rpc *solanarpc.Client
}

func (c *rpcLedgerClient) GetRecordData(ctx context.Context, account solana.PublicKey) ([]byte, error) {
	result, err := c.rpc.GetAccountInfo(ctx, account)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Value == nil {
		return nil, ErrAccountNotFound
	}
	return result.Value.Data.GetBinary(), nil
}

func compatNetworkConfig(t *testing.T) *config.NetworkConfig {
	t.Helper()
	cfg, err := config.NetworkConfigForEnv(config.EnvMainnetBeta)
	if err != nil {
		t.Fatalf("NetworkConfigForEnv: %v", err)
	}
	return cfg
}

func compatClient(t *testing.T) (*Client, solana.PublicKey) {
	t.Helper()
	cfg := compatNetworkConfig(t)
	rpcClient := solanarpc.New(cfg.SolanaRPCURL)
	ledger := &rpcLedgerClient{rpc: solanarpc.New(cfg.LedgerPublicRPCURL)}
	return NewWithLedger(rpcClient, cfg.RevenueDistributionProgramID, ledger), cfg.RevenueDistributionProgramID
}

func fetchRawAccount(t *testing.T, rpcClient *solanarpc.Client, addr solana.PublicKey) []byte {
	t.Helper()
	result, err := rpcClient.GetAccountInfo(context.Background(), addr)
	if err != nil {
		t.Fatalf("fetching %s: %v", addr, err)
	}
	if result == nil || result.Value == nil {
		t.Fatalf("account %s not found", addr)
	}
	return result.Value.Data.GetBinary()
}

func TestCompatProgramConfig(t *testing.T) {
	skipUnlessCompat(t)
	client, programID := compatClient(t)
	ctx := context.Background()

	progConfig, err := client.FetchConfig(ctx)
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}

	// Fetch raw bytes for independent verification.
	cfg := compatNetworkConfig(t)
	addr, _, _ := DeriveConfigPDA(programID)
	raw := fetchRawAccount(t, solanarpc.New(cfg.SolanaRPCURL), addr)

	// Verify discriminator.
	if err := validateDiscriminator(raw, DiscriminatorProgramConfig); err != nil {
		t.Fatalf("discriminator: %v", err)
	}

	// Verify fields at known raw byte offsets (offset = struct_offset + 8 for discriminator).
	assertU64(t, raw, 8, progConfig.Flags, "Flags")
	assertU64(t, raw, 16, progConfig.NextCompletedDZEpoch, "NextCompletedDZEpoch")
	assertU8(t, raw, 24, progConfig.BumpSeed, "BumpSeed")
	assertPubkey(t, raw, 32, progConfig.AdminKey, "AdminKey")
	assertPubkey(t, raw, 64, progConfig.DebtAccountantKey, "DebtAccountantKey")
	assertPubkey(t, raw, 96, progConfig.RewardsAccountantKey, "RewardsAccountantKey")
	assertPubkey(t, raw, 128, progConfig.ContributorManagerKey, "ContributorManagerKey")
	assertPubkey(t, raw, 192, progConfig.SOL2ZSwapProgramID, "SOL2ZSwapProgramID")

	// DistributionParameters starts at raw offset 224.
	dp := progConfig.DistributionParameters
	assertU16(t, raw, 224, dp.CalculationGracePeriodMinutes, "CalculationGracePeriodMinutes")
	assertU16(t, raw, 226, dp.InitializationGracePeriodMinutes, "InitializationGracePeriodMinutes")
	assertU8(t, raw, 228, dp.MinimumEpochDurationToFinalizeRewards, "MinEpochDuration")

	// CommunityBurnRateParameters at raw offset 232.
	cb := dp.CommunityBurnRateParameters
	assertU32(t, raw, 232, cb.Limit, "BurnRateLimit")
	assertU32(t, raw, 236, cb.DZEpochsToIncreasing, "DZEpochsToIncreasing")
	assertU32(t, raw, 240, cb.DZEpochsToLimit, "DZEpochsToLimit")

	// SolanaValidatorFeeParameters at raw offset 256.
	vf := dp.SolanaValidatorFeeParameters
	assertU16(t, raw, 256, vf.BaseBlockRewardsPct, "BaseBlockRewardsPct")
	assertU16(t, raw, 258, vf.PriorityBlockRewardsPct, "PriorityBlockRewardsPct")
	assertU16(t, raw, 260, vf.InflationRewardsPct, "InflationRewardsPct")
	assertU16(t, raw, 262, vf.JitoTipsPct, "JitoTipsPct")
	assertU32(t, raw, 264, vf.FixedSOLAmount, "FixedSOLAmount")

	// RelayParameters at raw offset 552 (224 + 328).
	rp := progConfig.RelayParameters
	assertU32(t, raw, 552, rp.PlaceholderLamports, "PlaceholderLamports")
	assertU32(t, raw, 556, rp.DistributeRewardsLamports, "DistributeRewardsLamports")

	// DebtWriteOffFeatureActivationEpoch at raw offset 600 (552 + 40 + 4 + 4).
	assertU64(t, raw, 600, progConfig.DebtWriteOffFeatureActivationEpoch, "DebtWriteOffEpoch")

	// Sanity: epoch should be > 0 on mainnet.
	if progConfig.NextCompletedDZEpoch == 0 {
		t.Error("NextCompletedDZEpoch is 0, expected > 0 on mainnet")
	}
}

func TestCompatDistribution(t *testing.T) {
	skipUnlessCompat(t)
	client, programID := compatClient(t)
	ctx := context.Background()

	// Fetch config to get the latest epoch.
	progConfig, err := client.FetchConfig(ctx)
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	epoch := progConfig.NextCompletedDZEpoch - 1

	dist, err := client.FetchDistribution(ctx, epoch)
	if err != nil {
		t.Fatalf("FetchDistribution(%d): %v", epoch, err)
	}

	cfg := compatNetworkConfig(t)
	addr, _, _ := DeriveDistributionPDA(programID, epoch)
	raw := fetchRawAccount(t, solanarpc.New(cfg.SolanaRPCURL), addr)

	if err := validateDiscriminator(raw, DiscriminatorDistribution); err != nil {
		t.Fatalf("discriminator: %v", err)
	}

	assertU64(t, raw, 8, dist.DZEpoch, "DZEpoch")
	if dist.DZEpoch != epoch {
		t.Errorf("DZEpoch = %d, want %d", dist.DZEpoch, epoch)
	}
	assertU64(t, raw, 16, dist.Flags, "Flags")
	assertU32(t, raw, 24, dist.CommunityBurnRate, "CommunityBurnRate")

	// SolanaValidatorFeeParameters at raw offset 32 (struct offset 24), 40 bytes.
	vf := dist.SolanaValidatorFeeParameters
	assertU16(t, raw, 32, vf.BaseBlockRewardsPct, "BaseBlockRewardsPct")
	assertU16(t, raw, 34, vf.PriorityBlockRewardsPct, "PriorityBlockRewardsPct")
	assertU16(t, raw, 36, vf.InflationRewardsPct, "InflationRewardsPct")
	assertU16(t, raw, 38, vf.JitoTipsPct, "JitoTipsPct")
	assertU32(t, raw, 40, vf.FixedSOLAmount, "FixedSOLAmount")

	// SolanaValidatorDebtMerkleRoot at raw offset 72 (32 bytes), skip direct comparison.
	assertU32(t, raw, 104, dist.TotalSolanaValidators, "TotalSolanaValidators")
	assertU32(t, raw, 108, dist.SolanaValidatorPaymentsCount, "SolanaValidatorPaymentsCount")
	assertU64(t, raw, 112, dist.TotalSolanaValidatorDebt, "TotalSolanaValidatorDebt")
	assertU64(t, raw, 120, dist.CollectedSolanaValidatorPayments, "CollectedPayments")
	// RewardsMerkleRoot at raw offset 128 (32 bytes).
	assertU32(t, raw, 160, dist.TotalContributors, "TotalContributors")
	assertU32(t, raw, 164, dist.DistributedRewardsCount, "DistributedRewardsCount")
	assertU64(t, raw, 168, dist.CollectedPrepaid2ZPayments, "CollectedPrepaid2ZPayments")
	assertU64(t, raw, 176, dist.Collected2ZConvertedFromSOL, "Collected2ZConvertedFromSOL")
	assertU64(t, raw, 184, dist.UncollectibleSOLDebt, "UncollectibleSOLDebt")
	assertU64(t, raw, 216, dist.Distributed2ZAmount, "Distributed2ZAmount")
	assertU64(t, raw, 224, dist.Burned2ZAmount, "Burned2ZAmount")
}

func TestCompatJournal(t *testing.T) {
	skipUnlessCompat(t)
	client, programID := compatClient(t)
	ctx := context.Background()

	journal, err := client.FetchJournal(ctx)
	if err != nil {
		t.Fatalf("FetchJournal: %v", err)
	}

	cfg := compatNetworkConfig(t)
	addr, _, _ := DeriveJournalPDA(programID)
	raw := fetchRawAccount(t, solanarpc.New(cfg.SolanaRPCURL), addr)

	if err := validateDiscriminator(raw, DiscriminatorJournal); err != nil {
		t.Fatalf("discriminator: %v", err)
	}

	assertU8(t, raw, 8, journal.BumpSeed, "BumpSeed")
	assertU64(t, raw, 16, journal.TotalSOLBalance, "TotalSOLBalance")
	assertU64(t, raw, 24, journal.Total2ZBalance, "Total2ZBalance")
	assertU64(t, raw, 32, journal.Swap2ZDestinationBalance, "Swap2ZDestinationBalance")
	assertU64(t, raw, 40, journal.SwappedSOLAmount, "SwappedSOLAmount")
	assertU64(t, raw, 48, journal.NextDZEpochToSweepTokens, "NextDZEpochToSweepTokens")
}

func TestCompatValidatorDeposit(t *testing.T) {
	skipUnlessCompat(t)
	client, _ := compatClient(t)
	ctx := context.Background()

	deposits, err := client.FetchAllValidatorDeposits(ctx)
	if err != nil {
		t.Fatalf("FetchAllValidatorDeposits: %v", err)
	}
	if len(deposits) == 0 {
		t.Fatal("no deposits found on mainnet")
	}

	// Verify we can look up a specific deposit by its node ID.
	first := deposits[0]
	single, err := client.FetchValidatorDeposit(ctx, first.NodeID)
	if err != nil {
		t.Fatalf("FetchValidatorDeposit(%s): %v", first.NodeID, err)
	}
	if single.NodeID != first.NodeID {
		t.Errorf("NodeID mismatch: single=%s, list=%s", single.NodeID, first.NodeID)
	}
	if single.WrittenOffSOLDebt != first.WrittenOffSOLDebt {
		t.Errorf("WrittenOffSOLDebt mismatch: single=%d, list=%d", single.WrittenOffSOLDebt, first.WrittenOffSOLDebt)
	}

	t.Logf("validated %d deposits, spot-checked %s", len(deposits), first.NodeID)
}

func TestCompatContributorRewards(t *testing.T) {
	skipUnlessCompat(t)
	client, _ := compatClient(t)
	ctx := context.Background()

	rewards, err := client.FetchAllContributorRewards(ctx)
	if err != nil {
		t.Fatalf("FetchAllContributorRewards: %v", err)
	}
	if len(rewards) == 0 {
		t.Fatal("no contributor rewards found on mainnet")
	}

	// Verify single lookup matches list.
	first := rewards[0]
	single, err := client.FetchContributorRewards(ctx, first.ServiceKey)
	if err != nil {
		t.Fatalf("FetchContributorRewards(%s): %v", first.ServiceKey, err)
	}
	if single.ServiceKey != first.ServiceKey {
		t.Errorf("ServiceKey mismatch")
	}
	if single.RewardsManagerKey != first.RewardsManagerKey {
		t.Errorf("RewardsManagerKey mismatch")
	}
	if single.Flags != first.Flags {
		t.Errorf("Flags mismatch")
	}

	t.Logf("validated %d contributors, spot-checked %s", len(rewards), first.ServiceKey)
}

func TestCompatValidatorDebts(t *testing.T) {
	skipUnlessCompat(t)
	client, _ := compatClient(t)
	ctx := context.Background()

	// Fetch config to get a completed epoch.
	progConfig, err := client.FetchConfig(ctx)
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	epoch := progConfig.NextCompletedDZEpoch - 1

	debts, err := client.FetchValidatorDebts(ctx, epoch)
	if err != nil {
		t.Fatalf("FetchValidatorDebts(%d): %v", epoch, err)
	}

	if debts.LastSolanaEpoch == 0 {
		t.Error("LastSolanaEpoch is 0, expected > 0 on mainnet")
	}
	if debts.FirstSolanaEpoch > debts.LastSolanaEpoch {
		t.Errorf("FirstSolanaEpoch (%d) > LastSolanaEpoch (%d)", debts.FirstSolanaEpoch, debts.LastSolanaEpoch)
	}

	t.Logf("epoch %d: %d validator debts, solana epochs %d-%d",
		epoch, len(debts.Debts), debts.FirstSolanaEpoch, debts.LastSolanaEpoch)
}

func TestCompatRewardShares(t *testing.T) {
	skipUnlessCompat(t)
	client, _ := compatClient(t)
	ctx := context.Background()

	// Fetch config to get a completed epoch.
	progConfig, err := client.FetchConfig(ctx)
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	epoch := progConfig.NextCompletedDZEpoch - 1

	shares, err := client.FetchRewardShares(ctx, epoch)
	if err != nil {
		t.Fatalf("FetchRewardShares(%d): %v", epoch, err)
	}

	if shares.Epoch != epoch {
		t.Errorf("Epoch = %d, want %d", shares.Epoch, epoch)
	}
	if len(shares.Rewards) == 0 {
		t.Error("no reward shares found on mainnet")
	}
	if shares.TotalUnitShares == 0 {
		t.Error("TotalUnitShares is 0, expected > 0")
	}

	t.Logf("epoch %d: %d reward shares, total unit shares %d",
		epoch, len(shares.Rewards), shares.TotalUnitShares)
}

// Helpers to compare deserialized values against raw byte reads.

func assertU8(t *testing.T, raw []byte, offset int, got uint8, name string) {
	t.Helper()
	want := raw[offset]
	if got != want {
		t.Errorf("%s: deserialized=%d, raw[%d]=%d", name, got, offset, want)
	}
}

func assertU16(t *testing.T, raw []byte, offset int, got uint16, name string) {
	t.Helper()
	want := binary.LittleEndian.Uint16(raw[offset:])
	if got != want {
		t.Errorf("%s: deserialized=%d, raw[%d]=%d", name, got, offset, want)
	}
}

func assertU32(t *testing.T, raw []byte, offset int, got uint32, name string) {
	t.Helper()
	want := binary.LittleEndian.Uint32(raw[offset:])
	if got != want {
		t.Errorf("%s: deserialized=%d, raw[%d]=%d", name, got, offset, want)
	}
}

func assertU64(t *testing.T, raw []byte, offset int, got uint64, name string) {
	t.Helper()
	want := binary.LittleEndian.Uint64(raw[offset:])
	if got != want {
		t.Errorf("%s: deserialized=%d, raw[%d]=%d", name, got, offset, want)
	}
}

func assertPubkey(t *testing.T, raw []byte, offset int, got solana.PublicKey, name string) {
	t.Helper()
	var want solana.PublicKey
	copy(want[:], raw[offset:offset+32])
	if got != want {
		t.Errorf("%s: deserialized=%s, raw[%d]=%s", name, got, offset, want)
	}
}
