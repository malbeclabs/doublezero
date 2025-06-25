package dzsdk

import (
	"bytes"
	"testing"

	"github.com/gagliardetto/solana-go"
)

func TestOrderPubkeys(t *testing.T) {
	// Create two test pubkeys
	pk1 := solana.NewWallet().PublicKey()
	pk2 := solana.NewWallet().PublicKey()

	// Test ordering is consistent
	orderedA1, orderedA2 := orderPubkeys(pk1, pk2)
	orderedB1, orderedB2 := orderPubkeys(pk2, pk1)

	if !orderedA1.Equals(orderedB1) || !orderedA2.Equals(orderedB2) {
		t.Error("orderPubkeys should return same order regardless of input order")
	}

	// Test that the smaller key comes first
	if bytes.Compare(orderedA1[:], orderedA2[:]) >= 0 {
		t.Error("orderPubkeys should return smaller key first")
	}
}

func TestOrderPubkeysSame(t *testing.T) {
	// Test with same pubkey
	pk := solana.NewWallet().PublicKey()
	ordered1, ordered2 := orderPubkeys(pk, pk)

	if !ordered1.Equals(pk) || !ordered2.Equals(pk) {
		t.Error("orderPubkeys with same key should return the same key twice")
	}
}

func TestDeriveDzLatencySamplesPDA(t *testing.T) {
	// Use a known program ID for testing
	programID := solana.NewWallet().PublicKey()
	deviceAPk := solana.NewWallet().PublicKey()
	deviceZPk := solana.NewWallet().PublicKey()
	linkPk := solana.NewWallet().PublicKey()
	epoch := uint64(100)

	// Derive PDA
	pda1, bump1, err := DeriveDzLatencySamplesPDA(programID, deviceAPk, deviceZPk, linkPk, epoch)
	if err != nil {
		t.Fatalf("Failed to derive PDA: %v", err)
	}

	// Verify PDA is not zero
	if pda1.IsZero() {
		t.Error("PDA should not be zero")
	}

	// Verify bump is valid (0-255)
	if bump1 > 255 {
		t.Errorf("Invalid bump seed: %d", bump1)
	}

	// Test that swapping device pubkeys produces same PDA
	pda2, bump2, err := DeriveDzLatencySamplesPDA(programID, deviceZPk, deviceAPk, linkPk, epoch)
	if err != nil {
		t.Fatalf("Failed to derive PDA with swapped keys: %v", err)
	}

	if !pda1.Equals(pda2) {
		t.Error("PDA should be the same regardless of device key order")
	}

	if bump1 != bump2 {
		t.Error("Bump seed should be the same regardless of device key order")
	}
}

func TestDeriveDzLatencySamplesPDADifferentEpochs(t *testing.T) {
	programID := solana.NewWallet().PublicKey()
	deviceAPk := solana.NewWallet().PublicKey()
	deviceZPk := solana.NewWallet().PublicKey()
	linkPk := solana.NewWallet().PublicKey()

	// Derive PDAs for different epochs
	pda1, _, err := DeriveDzLatencySamplesPDA(programID, deviceAPk, deviceZPk, linkPk, 100)
	if err != nil {
		t.Fatalf("Failed to derive PDA for epoch 100: %v", err)
	}

	pda2, _, err := DeriveDzLatencySamplesPDA(programID, deviceAPk, deviceZPk, linkPk, 101)
	if err != nil {
		t.Fatalf("Failed to derive PDA for epoch 101: %v", err)
	}

	// PDAs should be different for different epochs
	if pda1.Equals(pda2) {
		t.Error("PDAs should be different for different epochs")
	}
}

func TestDeriveDzLatencySamplesPDADifferentLinks(t *testing.T) {
	programID := solana.NewWallet().PublicKey()
	deviceAPk := solana.NewWallet().PublicKey()
	deviceZPk := solana.NewWallet().PublicKey()
	linkPk1 := solana.NewWallet().PublicKey()
	linkPk2 := solana.NewWallet().PublicKey()
	epoch := uint64(100)

	// Derive PDAs for different links
	pda1, _, err := DeriveDzLatencySamplesPDA(programID, deviceAPk, deviceZPk, linkPk1, epoch)
	if err != nil {
		t.Fatalf("Failed to derive PDA for link1: %v", err)
	}

	pda2, _, err := DeriveDzLatencySamplesPDA(programID, deviceAPk, deviceZPk, linkPk2, epoch)
	if err != nil {
		t.Fatalf("Failed to derive PDA for link2: %v", err)
	}

	// PDAs should be different for different links
	if pda1.Equals(pda2) {
		t.Error("PDAs should be different for different links")
	}
}
