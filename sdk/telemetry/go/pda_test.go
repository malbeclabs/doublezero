package telemetry

import (
	"testing"

	"github.com/gagliardetto/solana-go"
)

var testProgramID = solana.MustPublicKeyFromBase58("tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC")

func TestDeriveDeviceLatencySamplesPDA(t *testing.T) {
	origin := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
	target := solana.MustPublicKeyFromBase58("11111111111111111111111111111113")
	link := solana.MustPublicKeyFromBase58("11111111111111111111111111111114")

	addr, bump, err := DeriveDeviceLatencySamplesPDA(testProgramID, origin, target, link, 42)
	if err != nil {
		t.Fatalf("DeriveDeviceLatencySamplesPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}

	addr2, bump2, err := DeriveDeviceLatencySamplesPDA(testProgramID, origin, target, link, 42)
	if err != nil {
		t.Fatalf("DeriveDeviceLatencySamplesPDA (2nd): %v", err)
	}
	if addr != addr2 || bump != bump2 {
		t.Error("PDA derivation not deterministic")
	}
}

func TestDeriveInternetLatencySamplesPDA(t *testing.T) {
	oracle := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
	origin := solana.MustPublicKeyFromBase58("11111111111111111111111111111113")
	target := solana.MustPublicKeyFromBase58("11111111111111111111111111111114")

	addr, _, err := DeriveInternetLatencySamplesPDA(testProgramID, oracle, "RIPE Atlas", origin, target, 42)
	if err != nil {
		t.Fatalf("DeriveInternetLatencySamplesPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
}
