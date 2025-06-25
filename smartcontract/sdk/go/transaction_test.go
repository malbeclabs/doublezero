package dzsdk

import (
	"testing"

	"github.com/gagliardetto/solana-go"
)

func TestBuildInitializeDzLatencySamplesInstruction(t *testing.T) {
	// Create test keys
	programID := solana.NewWallet().PublicKey()
	telemetryProgramID := solana.NewWallet().PublicKey()
	signer := solana.NewWallet().PublicKey()
	deviceAPk := solana.NewWallet().PublicKey()
	deviceZPk := solana.NewWallet().PublicKey()
	linkPk := solana.NewWallet().PublicKey()

	args := &InitializeDzLatencySamplesArgs{
		DeviceAPk:                    deviceAPk,
		DeviceZPk:                    deviceZPk,
		LinkPk:                       linkPk,
		Epoch:                        100,
		SamplingIntervalMicroseconds: 1000000,
	}

	// Build instruction
	instruction, err := BuildInitializeDzLatencySamplesInstruction(
		programID,
		telemetryProgramID,
		signer,
		args,
	)
	if err != nil {
		t.Fatalf("Failed to build instruction: %v", err)
	}

	// Verify program ID
	if !instruction.ProgramID().Equals(telemetryProgramID) {
		t.Error("Instruction program ID mismatch")
	}

	// Verify number of accounts (7 total)
	expectedAccounts := 7
	if len(instruction.Accounts()) != expectedAccounts {
		t.Errorf("Expected %d accounts, got %d", expectedAccounts, len(instruction.Accounts()))
	}

	// Verify signer account
	signerAccount := instruction.Accounts()[1]
	if !signerAccount.PublicKey.Equals(signer) {
		t.Error("Signer account mismatch")
	}
	if !signerAccount.IsSigner {
		t.Error("Signer account should be marked as signer")
	}
	if !signerAccount.IsWritable {
		t.Error("Signer account should be writable")
	}

	// Verify system program
	systemProgramAccount := instruction.Accounts()[5]
	if !systemProgramAccount.PublicKey.Equals(solana.SystemProgramID) {
		t.Error("System program account mismatch")
	}

	// Verify serviceability program
	serviceabilityAccount := instruction.Accounts()[6]
	if !serviceabilityAccount.PublicKey.Equals(programID) {
		t.Error("Serviceability program account mismatch")
	}
}

func TestBuildWriteDzLatencySamplesInstruction(t *testing.T) {
	// Create test keys
	telemetryProgramID := solana.NewWallet().PublicKey()
	latencySamplesAccount := solana.NewWallet().PublicKey()
	signer := solana.NewWallet().PublicKey()

	samples := []uint32{100, 200, 300}
	args := &WriteDzLatencySamplesArgs{
		StartTimestampMicroseconds: 1234567890,
		Samples:                    samples,
	}

	// Build instruction
	instruction, err := BuildWriteDzLatencySamplesInstruction(
		telemetryProgramID,
		latencySamplesAccount,
		signer,
		args,
	)
	if err != nil {
		t.Fatalf("Failed to build instruction: %v", err)
	}

	// Verify program ID
	if !instruction.ProgramID().Equals(telemetryProgramID) {
		t.Error("Instruction program ID mismatch")
	}

	// Verify number of accounts (2 total)
	expectedAccounts := 2
	if len(instruction.Accounts()) != expectedAccounts {
		t.Errorf("Expected %d accounts, got %d", expectedAccounts, len(instruction.Accounts()))
	}

	// Verify latency samples account
	samplesAccount := instruction.Accounts()[0]
	if !samplesAccount.PublicKey.Equals(latencySamplesAccount) {
		t.Error("Latency samples account mismatch")
	}
	if samplesAccount.IsSigner {
		t.Error("Latency samples account should not be a signer")
	}
	if !samplesAccount.IsWritable {
		t.Error("Latency samples account should be writable")
	}

	// Verify signer account
	signerAccount := instruction.Accounts()[1]
	if !signerAccount.PublicKey.Equals(signer) {
		t.Error("Signer account mismatch")
	}
	if !signerAccount.IsSigner {
		t.Error("Signer account should be marked as signer")
	}
	if signerAccount.IsWritable {
		t.Error("Signer account should not be writable for write instruction")
	}
}
