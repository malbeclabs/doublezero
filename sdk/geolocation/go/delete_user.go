package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type DeleteGeolocationUserInstructionConfig struct {
	Code                      string
	ServiceabilityGlobalState solana.PublicKey
}

func (c *DeleteGeolocationUserInstructionConfig) Validate() error {
	if c.Code == "" {
		return fmt.Errorf("code is required")
	}
	if len(c.Code) > MaxCodeLength {
		return fmt.Errorf("code length %d exceeds max %d", len(c.Code), MaxCodeLength)
	}
	if c.ServiceabilityGlobalState.IsZero() {
		return fmt.Errorf("serviceability global state public key is required")
	}
	return nil
}

func BuildDeleteGeolocationUserInstruction(
	programID solana.PublicKey,
	signerPK solana.PublicKey,
	config DeleteGeolocationUserInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	// Serialize the instruction data.
	data, err := borsh.Serialize(struct {
		Discriminator uint8
	}{
		Discriminator: uint8(DeleteGeolocationUserInstructionIndex),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	// Derive PDAs.
	userPDA, _, err := DeriveGeolocationUserPDA(programID, config.Code)
	if err != nil {
		return nil, fmt.Errorf("failed to derive user PDA: %w", err)
	}
	configPDA, _, err := DeriveProgramConfigPDA(programID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive config PDA: %w", err)
	}

	// Build accounts.
	accounts := []*solana.AccountMeta{
		{PublicKey: userPDA, IsSigner: false, IsWritable: true},
		{PublicKey: configPDA, IsSigner: false, IsWritable: false},
		{PublicKey: config.ServiceabilityGlobalState, IsSigner: false, IsWritable: false},
		{PublicKey: signerPK, IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
