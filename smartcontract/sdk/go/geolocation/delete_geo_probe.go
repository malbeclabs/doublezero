package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type DeleteGeoProbeInstructionConfig struct {
	Payer   solana.PublicKey
	ProbePK solana.PublicKey
}

func (c *DeleteGeoProbeInstructionConfig) Validate() error {
	if c.Payer.IsZero() {
		return fmt.Errorf("payer public key is required")
	}
	if c.ProbePK.IsZero() {
		return fmt.Errorf("probe public key is required")
	}
	return nil
}

func BuildDeleteGeoProbeInstruction(
	programID solana.PublicKey,
	config DeleteGeoProbeInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	data, err := borsh.Serialize(struct {
		Discriminator uint8
	}{
		Discriminator: uint8(DeleteGeoProbeInstructionIndex),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	accounts := []*solana.AccountMeta{
		{PublicKey: config.ProbePK, IsSigner: false, IsWritable: true},
		{PublicKey: config.Payer, IsSigner: true, IsWritable: true},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
