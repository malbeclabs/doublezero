package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type InitProgramConfigInstructionConfig struct {
	Payer                   solana.PublicKey
	ServiceabilityProgramID solana.PublicKey
}

func (c *InitProgramConfigInstructionConfig) Validate() error {
	if c.Payer.IsZero() {
		return fmt.Errorf("payer public key is required")
	}
	if c.ServiceabilityProgramID.IsZero() {
		return fmt.Errorf("serviceability program ID is required")
	}
	return nil
}

func BuildInitProgramConfigInstruction(
	programID solana.PublicKey,
	config InitProgramConfigInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	data, err := borsh.Serialize(struct {
		Discriminator           uint8
		ServiceabilityProgramID solana.PublicKey
	}{
		Discriminator:           uint8(InitProgramConfigInstructionIndex),
		ServiceabilityProgramID: config.ServiceabilityProgramID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	programConfigPDA, _, err := DeriveProgramConfigPDA(programID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive program config PDA: %w", err)
	}

	programDataPDA, _, err := DeriveProgramDataPDA(programID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive program data PDA: %w", err)
	}

	accounts := []*solana.AccountMeta{
		{PublicKey: programConfigPDA, IsSigner: false, IsWritable: true},
		{PublicKey: config.Payer, IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
		{PublicKey: programDataPDA, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
