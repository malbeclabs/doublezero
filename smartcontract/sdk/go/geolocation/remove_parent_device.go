package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type RemoveParentDeviceInstructionConfig struct {
	Payer    solana.PublicKey
	ProbePK  solana.PublicKey
	DevicePK solana.PublicKey
}

func (c *RemoveParentDeviceInstructionConfig) Validate() error {
	if c.Payer.IsZero() {
		return fmt.Errorf("payer public key is required")
	}
	if c.ProbePK.IsZero() {
		return fmt.Errorf("probe public key is required")
	}
	if c.DevicePK.IsZero() {
		return fmt.Errorf("device public key is required")
	}
	return nil
}

func BuildRemoveParentDeviceInstruction(
	programID solana.PublicKey,
	config RemoveParentDeviceInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	data, err := borsh.Serialize(struct {
		Discriminator uint8
		DevicePK      solana.PublicKey
	}{
		Discriminator: uint8(RemoveParentDeviceInstructionIndex),
		DevicePK:      config.DevicePK,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	accounts := []*solana.AccountMeta{
		{PublicKey: config.ProbePK, IsSigner: false, IsWritable: true},
		{PublicKey: config.Payer, IsSigner: true, IsWritable: false},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
