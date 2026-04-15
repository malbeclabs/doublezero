package shreds

import (
	"context"
	"encoding/binary"
	"os"
	"testing"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

// These tests fetch live mainnet data and verify that our struct deserialization
// matches raw byte reads at known offsets. Run with:
//
//	SHREDS_COMPAT_TEST=1 go test -run TestCompat -v ./sdk/shreds/go/

func skipUnlessCompat(t *testing.T) {
	t.Helper()
	if os.Getenv("SHREDS_COMPAT_TEST") == "" {
		t.Skip("set SHREDS_COMPAT_TEST=1 to run compatibility tests against mainnet")
	}
}

func compatRPCClient(t *testing.T) *solanarpc.Client {
	t.Helper()
	url := SolanaRPCURLs["mainnet-beta"]
	if envURL := os.Getenv("SHREDS_RPC_URL"); envURL != "" {
		url = envURL
	}
	return NewRPCClient(url)
}

func compatClient(t *testing.T) *Client {
	t.Helper()
	return New(compatRPCClient(t), ProgramID)
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
	client := compatClient(t)
	ctx := context.Background()

	config, err := client.FetchProgramConfig(ctx)
	if err != nil {
		t.Fatalf("FetchProgramConfig: %v", err)
	}

	addr, _, _ := DeriveProgramConfigPDA(ProgramID)
	raw := fetchRawAccount(t, compatRPCClient(t), addr)

	if err := validateDiscriminator(raw, DiscriminatorProgramConfig); err != nil {
		t.Fatalf("discriminator: %v", err)
	}

	assertU64(t, raw, 8, config.Flags, "Flags")
	assertPubkey(t, raw, 16, config.AdminKey, "AdminKey")
	assertU32(t, raw, 48, config.ClosedForRequestsGracePeriodSlots, "GracePeriodSlots")
	assertU16(t, raw, 52, config.USDC2ZMaxSlippageBps, "MaxSlippageBps")
	assertPubkey(t, raw, 56, config.ShredOracleKey, "ShredOracleKey")
	assertPubkey(t, raw, 88, config.USDC2ZOracleKey, "USDC2ZOracleKey")

	t.Logf("admin=%s, oracle=%s", config.AdminKey, config.ShredOracleKey)
}

func TestCompatExecutionController(t *testing.T) {
	skipUnlessCompat(t)
	client := compatClient(t)
	ctx := context.Background()

	ec, err := client.FetchExecutionController(ctx)
	if err != nil {
		t.Fatalf("FetchExecutionController: %v", err)
	}

	addr, _, _ := DeriveExecutionControllerPDA(ProgramID)
	raw := fetchRawAccount(t, compatRPCClient(t), addr)

	if err := validateDiscriminator(raw, DiscriminatorExecutionController); err != nil {
		t.Fatalf("discriminator: %v", err)
	}

	assertU8(t, raw, 8, ec.Phase, "Phase")
	assertU16(t, raw, 12, ec.TotalMetros, "TotalMetros")
	assertU16(t, raw, 14, ec.TotalEnabledDevices, "TotalEnabledDevices")
	assertU32(t, raw, 16, ec.TotalClientSeats, "TotalClientSeats")
	assertU64(t, raw, 32, ec.CurrentSubscriptionEpoch, "CurrentSubscriptionEpoch")

	if ec.CurrentSubscriptionEpoch == 0 {
		t.Error("CurrentSubscriptionEpoch is 0, expected > 0 on mainnet")
	}

	t.Logf("epoch=%d, phase=%s, metros=%d, devices=%d, seats=%d",
		ec.CurrentSubscriptionEpoch, ec.GetPhase(), ec.TotalMetros,
		ec.TotalEnabledDevices, ec.TotalClientSeats)
}

func TestCompatMetroHistories(t *testing.T) {
	skipUnlessCompat(t)
	client := compatClient(t)
	ctx := context.Background()

	metros, err := client.FetchAllMetroHistories(ctx)
	if err != nil {
		t.Fatalf("FetchAllMetroHistories: %v", err)
	}
	if len(metros) == 0 {
		t.Fatal("no metro histories found on mainnet")
	}

	for _, m := range metros {
		if m.Prices.TotalCount == 0 {
			continue
		}
		idx := m.Prices.CurrentIndex
		entry := m.Prices.Entries[idx]
		t.Logf("metro %s: %d devices, current price $%d (epoch %d)",
			m.Pubkey, m.TotalInitializedDevices,
			entry.Price.USDCPriceDollars, entry.Epoch)
	}

	t.Logf("validated %d metro histories", len(metros))
}

func TestCompatDeviceHistories(t *testing.T) {
	skipUnlessCompat(t)
	client := compatClient(t)
	ctx := context.Background()

	devices, err := client.FetchAllDeviceHistories(ctx)
	if err != nil {
		t.Fatalf("FetchAllDeviceHistories: %v", err)
	}
	if len(devices) == 0 {
		t.Fatal("no device histories found on mainnet")
	}

	enabled := 0
	for _, d := range devices {
		if d.IsEnabled() {
			enabled++
		}
	}

	t.Logf("validated %d device histories (%d enabled)", len(devices), enabled)
}

func TestCompatClientSeats(t *testing.T) {
	skipUnlessCompat(t)
	client := compatClient(t)
	ctx := context.Background()

	seats, err := client.FetchAllClientSeats(ctx)
	if err != nil {
		t.Fatalf("FetchAllClientSeats: %v", err)
	}

	funded := 0
	for _, s := range seats {
		if s.FundedEpoch > 0 {
			funded++
		}
	}

	t.Logf("validated %d client seats (%d funded)", len(seats), funded)
}

func TestCompatPaymentEscrows(t *testing.T) {
	skipUnlessCompat(t)
	client := compatClient(t)
	ctx := context.Background()

	escrows, err := client.FetchAllPaymentEscrows(ctx)
	if err != nil {
		t.Fatalf("FetchAllPaymentEscrows: %v", err)
	}

	totalUSDC := uint64(0)
	for _, e := range escrows {
		totalUSDC += e.USDCBalance
	}

	t.Logf("validated %d payment escrows, total USDC balance: %d", len(escrows), totalUSDC)
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
