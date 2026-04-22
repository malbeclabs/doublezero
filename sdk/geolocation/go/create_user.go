package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type CreateGeolocationUserInstructionConfig struct {
	Code         string
	TokenAccount solana.PublicKey
}

func (c *CreateGeolocationUserInstructionConfig) Validate() error {
	if c.Code == "" {
		return fmt.Errorf("code is required")
	}
	if len(c.Code) > MaxCodeLength {
		return fmt.Errorf("code length %d exceeds max %d", len(c.Code), MaxCodeLength)
	}
	return nil
}

func BuildCreateGeolocationUserInstruction(
	programID solana.PublicKey,
	signerPK solana.PublicKey,
	config CreateGeolocationUserInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	// Serialize the instruction data.
	data, err := borsh.Serialize(struct {
		Discriminator uint8
		Code          string
		TokenAccount  [32]byte
	}{
		Discriminator: uint8(CreateGeolocationUserInstructionIndex),
		Code:          config.Code,
		TokenAccount:  config.TokenAccount,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	// Derive the user PDA.
	userPDA, _, err := DeriveGeolocationUserPDA(programID, config.Code)
	if err != nil {
		return nil, fmt.Errorf("failed to derive user PDA: %w", err)
	}

	// Build accounts.
	accounts := []*solana.AccountMeta{
		{PublicKey: userPDA, IsSigner: false, IsWritable: true},
		{PublicKey: signerPK, IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
