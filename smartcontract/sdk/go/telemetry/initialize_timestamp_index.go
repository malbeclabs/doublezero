package telemetry

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

// BuildInitializeTimestampIndexInstruction builds the instruction for initializing
// a timestamp index companion account for a given samples account.
func BuildInitializeTimestampIndexInstruction(
	programID solana.PublicKey,
	agentPK solana.PublicKey,
	samplesAccountPK solana.PublicKey,
) (solana.Instruction, error) {
	if agentPK.IsZero() {
		return nil, fmt.Errorf("agent public key is required")
	}
	if samplesAccountPK.IsZero() {
		return nil, fmt.Errorf("samples account public key is required")
	}

	data, err := borsh.Serialize(struct {
		Discriminator uint8
	}{
		Discriminator: uint8(InitializeTimestampIndexInstructionIndex),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	timestampIndexPDA, _, err := DeriveTimestampIndexPDA(programID, samplesAccountPK)
	if err != nil {
		return nil, fmt.Errorf("failed to derive timestamp index PDA: %w", err)
	}

	accounts := []*solana.AccountMeta{
		{PublicKey: timestampIndexPDA, IsSigner: false, IsWritable: true},
		{PublicKey: samplesAccountPK, IsSigner: false, IsWritable: false},
		{PublicKey: agentPK, IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
