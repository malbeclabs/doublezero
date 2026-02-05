package serviceability

import (
	"testing"

	"github.com/gagliardetto/solana-go"
)

var testProgramID = solana.MustPublicKeyFromBase58("ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv")

func TestDeriveGlobalStatePDA(t *testing.T) {
	addr, bump, err := DeriveGlobalStatePDA(testProgramID)
	if err != nil {
		t.Fatalf("DeriveGlobalStatePDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
	if bump == 0 {
		t.Log("bump is 0 (valid but unusual)")
	}

	addr2, bump2, err := DeriveGlobalStatePDA(testProgramID)
	if err != nil {
		t.Fatalf("DeriveGlobalStatePDA (2nd): %v", err)
	}
	if addr != addr2 || bump != bump2 {
		t.Error("PDA derivation not deterministic")
	}
}

func TestDeriveGlobalConfigPDA(t *testing.T) {
	addr, _, err := DeriveGlobalConfigPDA(testProgramID)
	if err != nil {
		t.Fatalf("DeriveGlobalConfigPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
}

func TestDeriveProgramConfigPDA(t *testing.T) {
	addr, _, err := DeriveProgramConfigPDA(testProgramID)
	if err != nil {
		t.Fatalf("DeriveProgramConfigPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
}

func TestPDAsAreDifferent(t *testing.T) {
	gs, _, _ := DeriveGlobalStatePDA(testProgramID)
	gc, _, _ := DeriveGlobalConfigPDA(testProgramID)
	pc, _, _ := DeriveProgramConfigPDA(testProgramID)
	if gs == gc || gs == pc || gc == pc {
		t.Error("different PDA types produced same address")
	}
}
