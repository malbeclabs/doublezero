package serviceability

import (
	"context"
	"encoding/binary"
	"os"
	"testing"

	"github.com/gagliardetto/solana-go"
)

// These tests fetch live mainnet data and verify that our struct deserialization
// matches raw byte reads at known offsets. Run with:
//
//	SERVICEABILITY_COMPAT_TEST=1 go test -run TestCompat -v ./sdk/serviceability/go/
//
// Requires network access to Solana mainnet RPC.

func skipUnlessCompat(t *testing.T) {
	t.Helper()
	if os.Getenv("SERVICEABILITY_COMPAT_TEST") == "" {
		t.Skip("set SERVICEABILITY_COMPAT_TEST=1 to run compatibility tests against mainnet")
	}
}

func compatRPCURL() string {
	if url := os.Getenv("SOLANA_RPC_URL"); url != "" {
		return url
	}
	return LedgerRPCURLs["mainnet-beta"]
}

func compatProgramID() solana.PublicKey {
	return solana.MustPublicKeyFromBase58(ProgramIDs["mainnet-beta"])
}

func fetchRawAccount(t *testing.T, addr solana.PublicKey) []byte {
	t.Helper()
	rpcClient := NewRPCClient(compatRPCURL())
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

	programID := compatProgramID()
	addr, _, err := DeriveProgramConfigPDA(programID)
	if err != nil {
		t.Fatalf("DeriveProgramConfigPDA: %v", err)
	}

	raw := fetchRawAccount(t, addr)
	reader := NewByteReader(raw)
	var pc ProgramConfig
	DeserializeProgramConfig(reader, &pc)

	// ProgramConfig layout (all fixed-size):
	// offset 0: AccountType (u8)
	// offset 1: BumpSeed (u8)
	// offset 2: Version.Major (u32)
	// offset 6: Version.Minor (u32)
	// offset 10: Version.Patch (u32)
	// offset 14: MinCompatVersion.Major (u32)
	// offset 18: MinCompatVersion.Minor (u32)
	// offset 22: MinCompatVersion.Patch (u32)
	compatAssertU8(t, raw, 0, uint8(pc.AccountType), "AccountType")
	compatAssertU8(t, raw, 1, pc.BumpSeed, "BumpSeed")
	compatAssertU32(t, raw, 2, pc.Version.Major, "Version.Major")
	compatAssertU32(t, raw, 6, pc.Version.Minor, "Version.Minor")
	compatAssertU32(t, raw, 10, pc.Version.Patch, "Version.Patch")
	compatAssertU32(t, raw, 14, pc.MinCompatVersion.Major, "MinCompatVersion.Major")
	compatAssertU32(t, raw, 18, pc.MinCompatVersion.Minor, "MinCompatVersion.Minor")
	compatAssertU32(t, raw, 22, pc.MinCompatVersion.Patch, "MinCompatVersion.Patch")

	if pc.AccountType != ProgramConfigType {
		t.Errorf("AccountType = %d, want %d", pc.AccountType, ProgramConfigType)
	}

	t.Logf("ProgramConfig: version %d.%d.%d, min compat %d.%d.%d",
		pc.Version.Major, pc.Version.Minor, pc.Version.Patch,
		pc.MinCompatVersion.Major, pc.MinCompatVersion.Minor, pc.MinCompatVersion.Patch)
}

func TestCompatGlobalConfig(t *testing.T) {
	skipUnlessCompat(t)

	programID := compatProgramID()
	addr, _, err := DeriveGlobalConfigPDA(programID)
	if err != nil {
		t.Fatalf("DeriveGlobalConfigPDA: %v", err)
	}

	raw := fetchRawAccount(t, addr)
	reader := NewByteReader(raw)
	var gc GlobalConfig
	DeserializeGlobalConfig(reader, &gc)

	// GlobalConfig layout (all fixed-size):
	// offset 0: AccountType (u8)
	// offset 1: Owner (32 bytes)
	// offset 33: BumpSeed (u8)
	// offset 34: LocalASN (u32)
	// offset 38: RemoteASN (u32)
	// offset 42: DeviceTunnelBlock (5 bytes)
	// offset 47: UserTunnelBlock (5 bytes)
	// offset 52: MulticastGroupBlock (5 bytes)
	// offset 57: NextBGPCommunity (u16)
	compatAssertU8(t, raw, 0, uint8(gc.AccountType), "AccountType")
	compatAssertPubkey(t, raw, 1, gc.Owner, "Owner")
	compatAssertU8(t, raw, 33, gc.BumpSeed, "BumpSeed")
	compatAssertU32(t, raw, 34, gc.LocalASN, "LocalASN")
	compatAssertU32(t, raw, 38, gc.RemoteASN, "RemoteASN")
	compatAssertU16(t, raw, 57, gc.NextBGPCommunity, "NextBGPCommunity")

	if gc.AccountType != GlobalConfigType {
		t.Errorf("AccountType = %d, want %d", gc.AccountType, GlobalConfigType)
	}
	if gc.LocalASN == 0 {
		t.Error("LocalASN is 0, expected > 0 on mainnet")
	}

	t.Logf("GlobalConfig: localASN=%d, remoteASN=%d, nextBGPCommunity=%d",
		gc.LocalASN, gc.RemoteASN, gc.NextBGPCommunity)
}

func TestCompatGlobalState(t *testing.T) {
	skipUnlessCompat(t)

	programID := compatProgramID()
	addr, _, err := DeriveGlobalStatePDA(programID)
	if err != nil {
		t.Fatalf("DeriveGlobalStatePDA: %v", err)
	}

	raw := fetchRawAccount(t, addr)
	reader := NewByteReader(raw)
	var gs GlobalState
	DeserializeGlobalState(reader, &gs)

	// GlobalState fixed layout (first 18 bytes before variable-length vecs):
	// offset 0: AccountType (u8)
	// offset 1: BumpSeed (u8)
	// offset 2: AccountIndex (u128 = 16 bytes)
	compatAssertU8(t, raw, 0, uint8(gs.AccountType), "AccountType")
	compatAssertU8(t, raw, 1, gs.BumpSeed, "BumpSeed")

	if gs.AccountType != GlobalStateType {
		t.Errorf("AccountType = %d, want %d", gs.AccountType, GlobalStateType)
	}

	// Sanity checks on deserialized values.
	if gs.AccountIndex.Low == 0 && gs.AccountIndex.High == 0 {
		t.Error("AccountIndex is zero, expected > 0 on mainnet")
	}
	var zeroPK [32]byte
	if gs.ActivatorAuthorityPK == zeroPK {
		t.Error("ActivatorAuthorityPK is zero")
	}
	if gs.SentinelAuthorityPK == zeroPK {
		t.Error("SentinelAuthorityPK is zero")
	}
	if gs.HealthOraclePK == zeroPK {
		t.Log("HealthOraclePK is zero")
	}

	t.Logf("GlobalState: accountIndex=%d, foundationAllowlist=%d entries, qaAllowlist=%d entries",
		gs.AccountIndex.Low, len(gs.FoundationAllowlist), len(gs.QAAllowlist))
}

func TestCompatGetProgramData(t *testing.T) {
	skipUnlessCompat(t)

	rpcClient := NewRPCClient(compatRPCURL())
	client := NewMainnetBeta(rpcClient)
	ctx := context.Background()

	pd, err := client.GetProgramData(ctx)
	if err != nil {
		t.Fatalf("GetProgramData: %v", err)
	}

	if pd.GlobalState == nil {
		t.Fatal("GlobalState is nil")
	}
	if pd.GlobalConfig == nil {
		t.Fatal("GlobalConfig is nil")
	}
	if pd.ProgramConfig == nil {
		t.Fatal("ProgramConfig is nil")
	}
	if len(pd.Locations) == 0 {
		t.Error("no locations found on mainnet")
	}
	if len(pd.Exchanges) == 0 {
		t.Error("no exchanges found on mainnet")
	}
	if len(pd.Devices) == 0 {
		t.Error("no devices found on mainnet")
	}
	if len(pd.Links) == 0 {
		t.Error("no links found on mainnet")
	}
	if len(pd.Contributors) == 0 {
		t.Error("no contributors found on mainnet")
	}

	t.Logf("ProgramData: %d locations, %d exchanges, %d devices, %d links, %d users, %d contributors, %d access passes",
		len(pd.Locations), len(pd.Exchanges), len(pd.Devices), len(pd.Links),
		len(pd.Users), len(pd.Contributors), len(pd.AccessPasses))
}

// Helpers to compare deserialized values against raw byte reads.

func compatAssertU8(t *testing.T, raw []byte, offset int, got uint8, name string) {
	t.Helper()
	want := raw[offset]
	if got != want {
		t.Errorf("%s: deserialized=%d, raw[%d]=%d", name, got, offset, want)
	}
}

func compatAssertU16(t *testing.T, raw []byte, offset int, got uint16, name string) {
	t.Helper()
	want := binary.LittleEndian.Uint16(raw[offset:])
	if got != want {
		t.Errorf("%s: deserialized=%d, raw[%d]=%d", name, got, offset, want)
	}
}

func compatAssertU32(t *testing.T, raw []byte, offset int, got uint32, name string) {
	t.Helper()
	want := binary.LittleEndian.Uint32(raw[offset:])
	if got != want {
		t.Errorf("%s: deserialized=%d, raw[%d]=%d", name, got, offset, want)
	}
}

func compatAssertPubkey(t *testing.T, raw []byte, offset int, got [32]byte, name string) {
	t.Helper()
	var want [32]byte
	copy(want[:], raw[offset:offset+32])
	if got != want {
		t.Errorf("%s: deserialized=%x, raw[%d]=%x", name, got, offset, want)
	}
}
