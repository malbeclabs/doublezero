package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type UpdateProgramConfigInstructionConfig struct {
	Payer                   solana.PublicKey
	ServiceabilityProgramID *solana.PublicKey
}

func (c *UpdateProgramConfigInstructionConfig) Validate() error {
	if c.Payer.IsZero() {
		return fmt.Errorf("payer public key is required")
	}
	return nil
}

func BuildUpdateProgramConfigInstruction(
	programID solana.PublicKey,
	config UpdateProgramConfigInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	type updateArgs struct {
		Discriminator           uint8
		ServiceabilityProgramID *solana.PublicKey `borsh_optional:"true"`
	}

	data, err := borsh.Serialize(updateArgs{
		Discriminator:           uint8(UpdateProgramConfigInstructionIndex),
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
		{PublicKey: config.Payer, IsSigner: true, IsWritable: false},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
		{PublicKey: programDataPDA, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
