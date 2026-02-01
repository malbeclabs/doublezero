package revdist

import (
	"testing"

	"github.com/gagliardetto/solana-go"
)

var testProgramID = solana.MustPublicKeyFromBase58("dzrevZC94tBLwuHw1dyynZxaXTWyp7yocsinyEVPtt4")

func TestDeriveConfigPDA(t *testing.T) {
	addr, bump, err := DeriveConfigPDA(testProgramID)
	if err != nil {
		t.Fatalf("DeriveConfigPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
	if bump == 0 {
		t.Log("bump is 0 (valid but unusual)")
	}

	// Deriving again should produce the same result.
	addr2, bump2, err := DeriveConfigPDA(testProgramID)
	if err != nil {
		t.Fatalf("DeriveConfigPDA (2nd): %v", err)
	}
	if addr != addr2 || bump != bump2 {
		t.Error("PDA derivation not deterministic")
	}
}

func TestDeriveDistributionPDA(t *testing.T) {
	addr1, _, err := DeriveDistributionPDA(testProgramID, 1)
	if err != nil {
		t.Fatalf("DeriveDistributionPDA epoch 1: %v", err)
	}
	addr2, _, err := DeriveDistributionPDA(testProgramID, 2)
	if err != nil {
		t.Fatalf("DeriveDistributionPDA epoch 2: %v", err)
	}
	if addr1 == addr2 {
		t.Error("different epochs produced same PDA")
	}
}

func TestDeriveValidatorDepositPDA(t *testing.T) {
	nodeID := solana.NewWallet().PublicKey()
	addr, _, err := DeriveValidatorDepositPDA(testProgramID, nodeID)
	if err != nil {
		t.Fatalf("DeriveValidatorDepositPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
}

func TestDeriveContributorRewardsPDA(t *testing.T) {
	serviceKey := solana.NewWallet().PublicKey()
	addr, _, err := DeriveContributorRewardsPDA(testProgramID, serviceKey)
	if err != nil {
		t.Fatalf("DeriveContributorRewardsPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
}

func TestDeriveJournalPDA(t *testing.T) {
	addr, _, err := DeriveJournalPDA(testProgramID)
	if err != nil {
		t.Fatalf("DeriveJournalPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
}

func TestCreateRecordSeedString(t *testing.T) {
	// Test vector from Rust: create_record_seed_string(&[b"test_create_record_seed_string"])
	// Expected: "8YGyrUprn2DwKkq3hR2DaqGPYDD5WE1D"
	got := createRecordSeedString([][]byte{[]byte("test_create_record_seed_string")})
	want := "8YGyrUprn2DwKkq3hR2DaqGPYDD5WE1D"
	if got != want {
		t.Errorf("createRecordSeedString = %q, want %q", got, want)
	}
	if len(got) != 32 {
		t.Errorf("seed string length = %d, want 32", len(got))
	}
}

func TestDeriveRecordKey(t *testing.T) {
	// Test vector from Rust: create_record_key(
	//   "84s5hmJUjfRhsQ443M1iWnCfNNmLbQLHmWTRyHtxbQzw",
	//   &[b"test_create_record_key"],
	// ) == "9eP3pWoN5uFfUsHBb63wgWnMPjbvGSzQgQe6EDRCdpKJ"
	payerKey := solana.MustPublicKeyFromBase58("84s5hmJUjfRhsQ443M1iWnCfNNmLbQLHmWTRyHtxbQzw")
	got, err := DeriveRecordKey(payerKey, [][]byte{[]byte("test_create_record_key")})
	if err != nil {
		t.Fatalf("DeriveRecordKey: %v", err)
	}
	want := solana.MustPublicKeyFromBase58("9eP3pWoN5uFfUsHBb63wgWnMPjbvGSzQgQe6EDRCdpKJ")
	if got != want {
		t.Errorf("DeriveRecordKey = %s, want %s", got, want)
	}
}
