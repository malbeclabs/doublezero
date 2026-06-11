package shreds

import (
	"testing"

	"github.com/gagliardetto/solana-go"
)

var testProgramID = solana.MustPublicKeyFromBase58("dzshrr3yL57SB13sJPYHYo3TV8Bo1i1FxkyrZr3bKNE")

func TestDeriveProgramConfigPDA(t *testing.T) {
	addr, _, err := DeriveProgramConfigPDA(testProgramID)
	if err != nil {
		t.Fatalf("DeriveProgramConfigPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
	// Deterministic.
	addr2, _, _ := DeriveProgramConfigPDA(testProgramID)
	if addr != addr2 {
		t.Error("PDA derivation not deterministic")
	}
}

func TestDeriveExecutionControllerPDA(t *testing.T) {
	addr, _, err := DeriveExecutionControllerPDA(testProgramID)
	if err != nil {
		t.Fatalf("DeriveExecutionControllerPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
}

func TestDeriveClientSeatPDA(t *testing.T) {
	device := solana.NewWallet().PublicKey()
	addr1, _, err := DeriveClientSeatPDA(testProgramID, device, 0x0A000001)
	if err != nil {
		t.Fatalf("DeriveClientSeatPDA: %v", err)
	}
	if addr1.IsZero() {
		t.Error("derived zero address")
	}

	// Different IP produces different PDA.
	addr2, _, _ := DeriveClientSeatPDA(testProgramID, device, 0x0A000002)
	if addr1 == addr2 {
		t.Error("different IPs produced same PDA")
	}

	// Different device produces different PDA.
	device2 := solana.NewWallet().PublicKey()
	addr3, _, _ := DeriveClientSeatPDA(testProgramID, device2, 0x0A000001)
	if addr1 == addr3 {
		t.Error("different devices produced same PDA")
	}
}

func TestDerivePaymentEscrowPDA(t *testing.T) {
	seat := solana.NewWallet().PublicKey()
	auth1 := solana.NewWallet().PublicKey()
	auth2 := solana.NewWallet().PublicKey()

	addr1, _, err := DerivePaymentEscrowPDA(testProgramID, seat, auth1)
	if err != nil {
		t.Fatalf("DerivePaymentEscrowPDA: %v", err)
	}
	if addr1.IsZero() {
		t.Error("derived zero address")
	}

	// Different authority produces different PDA.
	addr2, _, _ := DerivePaymentEscrowPDA(testProgramID, seat, auth2)
	if addr1 == addr2 {
		t.Error("different authorities produced same PDA")
	}
}

func TestDeriveShredDistributionPDA(t *testing.T) {
	addr1, _, err := DeriveShredDistributionPDA(testProgramID, 1)
	if err != nil {
		t.Fatalf("DeriveShredDistributionPDA: %v", err)
	}
	addr2, _, _ := DeriveShredDistributionPDA(testProgramID, 2)
	if addr1 == addr2 {
		t.Error("different epochs produced same PDA")
	}
}

func TestDeriveValidatorClientRewardsPDA(t *testing.T) {
	addr1, _, err := DeriveValidatorClientRewardsPDA(testProgramID, 1)
	if err != nil {
		t.Fatalf("DeriveValidatorClientRewardsPDA: %v", err)
	}
	addr2, _, _ := DeriveValidatorClientRewardsPDA(testProgramID, 2)
	if addr1 == addr2 {
		t.Error("different client IDs produced same PDA")
	}
}

func TestDeriveInstantSeatAllocationRequestPDA(t *testing.T) {
	device := solana.NewWallet().PublicKey()
	addr, _, err := DeriveInstantSeatAllocationRequestPDA(testProgramID, device, 0x0A000001)
	if err != nil {
		t.Fatalf("DeriveInstantSeatAllocationRequestPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
}

func TestDeriveWithdrawSeatRequestPDA(t *testing.T) {
	seat := solana.NewWallet().PublicKey()
	addr, _, err := DeriveWithdrawSeatRequestPDA(testProgramID, seat)
	if err != nil {
		t.Fatalf("DeriveWithdrawSeatRequestPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
}

func TestDeriveMetroHistoryPDA(t *testing.T) {
	exchange := solana.NewWallet().PublicKey()
	addr, _, err := DeriveMetroHistoryPDA(testProgramID, exchange)
	if err != nil {
		t.Fatalf("DeriveMetroHistoryPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
}

func TestDeriveDeviceHistoryPDA(t *testing.T) {
	device := solana.NewWallet().PublicKey()
	addr, _, err := DeriveDeviceHistoryPDA(testProgramID, device)
	if err != nil {
		t.Fatalf("DeriveDeviceHistoryPDA: %v", err)
	}
	if addr.IsZero() {
		t.Error("derived zero address")
	}
}

func TestSingletonPDAsAreDistinct(t *testing.T) {
	config, _, _ := DeriveProgramConfigPDA(testProgramID)
	exec, _, _ := DeriveExecutionControllerPDA(testProgramID)
	if config == exec {
		t.Error("ProgramConfig and ExecutionController PDAs collide")
	}
}
