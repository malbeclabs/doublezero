package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type UpdateGeolocationUserInstructionConfig struct {
	Code         string
	TokenAccount *solana.PublicKey // optional
}

func (c *UpdateGeolocationUserInstructionConfig) Validate() error {
	if c.Code == "" {
		return fmt.Errorf("code is required")
	}
	if len(c.Code) > MaxCodeLength {
		return fmt.Errorf("code length %d exceeds max %d", len(c.Code), MaxCodeLength)
	}
	if c.TokenAccount == nil {
		return fmt.Errorf("at least one field must be provided: TokenAccount is nil")
	}
	return nil
}

func BuildUpdateGeolocationUserInstruction(
	programID solana.PublicKey,
	signerPK solana.PublicKey,
	config UpdateGeolocationUserInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	// Serialize the instruction data with Option<Pubkey> encoding.
	var tokenAccountOpt *[32]byte
	if config.TokenAccount != nil {
		pk := [32]byte(*config.TokenAccount)
		tokenAccountOpt = &pk
	}

	data, err := borsh.Serialize(struct {
		Discriminator uint8
		TokenAccount  *[32]byte
	}{
		Discriminator: uint8(UpdateGeolocationUserInstructionIndex),
		TokenAccount:  tokenAccountOpt,
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
