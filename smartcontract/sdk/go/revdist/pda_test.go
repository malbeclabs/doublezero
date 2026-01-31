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
