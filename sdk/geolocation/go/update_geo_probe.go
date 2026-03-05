package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type UpdateGeoProbeInstructionConfig struct {
	Payer                       solana.PublicKey
	ProbePK                     solana.PublicKey
	ServiceabilityGlobalStatePK solana.PublicKey
	PublicIP                    *[4]uint8
	LocationOffsetPort          *uint16
	MetricsPublisherPK          *solana.PublicKey
}

func (c *UpdateGeoProbeInstructionConfig) Validate() error {
	if c.Payer.IsZero() {
		return fmt.Errorf("payer public key is required")
	}
	if c.ProbePK.IsZero() {
		return fmt.Errorf("probe public key is required")
	}
	if c.ServiceabilityGlobalStatePK.IsZero() {
		return fmt.Errorf("serviceability global state public key is required")
	}
	return nil
}

func BuildUpdateGeoProbeInstruction(
	programID solana.PublicKey,
	config UpdateGeoProbeInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	data, err := borsh.Serialize(struct {
		Discriminator      uint8
		PublicIP           *[4]uint8         `borsh_optional:"true"`
		LocationOffsetPort *uint16           `borsh_optional:"true"`
		MetricsPublisherPK *solana.PublicKey `borsh_optional:"true"`
	}{
		Discriminator:      uint8(UpdateGeoProbeInstructionIndex),
		PublicIP:           config.PublicIP,
		LocationOffsetPort: config.LocationOffsetPort,
		MetricsPublisherPK: config.MetricsPublisherPK,
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
		{PublicKey: programConfigPDA, IsSigner: false, IsWritable: false},
		{PublicKey: config.ServiceabilityGlobalStatePK, IsSigner: false, IsWritable: false},
		{PublicKey: config.Payer, IsSigner: true, IsWritable: true},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
