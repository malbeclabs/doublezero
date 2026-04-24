package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type AddTargetInstructionConfig struct {
	Code               string
	ProbePK            solana.PublicKey
	TargetType         GeoLocationTargetType
	IPAddress          [4]uint8
	LocationOffsetPort uint16
	TargetPK           solana.PublicKey
}

func (c *AddTargetInstructionConfig) Validate() error {
	if c.Code == "" {
		return fmt.Errorf("code is required")
	}
	if len(c.Code) > MaxCodeLength {
		return fmt.Errorf("code length %d exceeds max %d", len(c.Code), MaxCodeLength)
	}
	if c.ProbePK.IsZero() {
		return fmt.Errorf("probe public key is required")
	}

	switch c.TargetType {
	case GeoLocationTargetTypeOutbound, GeoLocationTargetTypeOutboundIcmp:
		if err := validatePublicIP(c.IPAddress); err != nil {
			return err
		}
	case GeoLocationTargetTypeInbound:
		if c.TargetPK.IsZero() {
			return fmt.Errorf("target public key is required for inbound target type")
		}
	default:
		return fmt.Errorf("unknown target type: %d", c.TargetType)
	}

	return nil
}

func BuildAddTargetInstruction(
	programID solana.PublicKey,
	signerPK solana.PublicKey,
	config AddTargetInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	// Serialize the instruction data.
	data, err := borsh.Serialize(struct {
		Discriminator      uint8
		TargetType         uint8
		IPAddress          [4]uint8
		LocationOffsetPort uint16
		TargetPK           [32]byte
	}{
		Discriminator:      uint8(AddTargetInstructionIndex),
		TargetType:         uint8(config.TargetType),
		IPAddress:          config.IPAddress,
		LocationOffsetPort: config.LocationOffsetPort,
		TargetPK:           config.TargetPK,
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
		{PublicKey: config.ProbePK, IsSigner: false, IsWritable: true},
		{PublicKey: signerPK, IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
