package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type AddParentDeviceInstructionConfig struct {
	Payer                       solana.PublicKey
	ProbePK                     solana.PublicKey
	DevicePK                    solana.PublicKey
	ServiceabilityGlobalStatePK solana.PublicKey
}

func (c *AddParentDeviceInstructionConfig) Validate() error {
	if c.Payer.IsZero() {
		return fmt.Errorf("payer public key is required")
	}
	if c.ProbePK.IsZero() {
		return fmt.Errorf("probe public key is required")
	}
	if c.DevicePK.IsZero() {
		return fmt.Errorf("device public key is required")
	}
	if c.ServiceabilityGlobalStatePK.IsZero() {
		return fmt.Errorf("serviceability global state public key is required")
	}
	return nil
}

func BuildAddParentDeviceInstruction(
	programID solana.PublicKey,
	config AddParentDeviceInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	data, err := borsh.Serialize(struct {
		Discriminator uint8
	}{
		Discriminator: uint8(AddParentDeviceInstructionIndex),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	programConfigPDA, _, err := DeriveProgramConfigPDA(programID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive program config PDA: %w", err)
	}

	accounts := []*solana.AccountMeta{
		{PublicKey: config.ProbePK, IsSigner: false, IsWritable: true},
		{PublicKey: config.DevicePK, IsSigner: false, IsWritable: false},
		{PublicKey: programConfigPDA, IsSigner: false, IsWritable: false},
		{PublicKey: config.ServiceabilityGlobalStatePK, IsSigner: false, IsWritable: false},
		{PublicKey: config.Payer, IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
