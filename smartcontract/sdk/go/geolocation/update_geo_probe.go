package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type UpdateGeoProbeInstructionConfig struct {
	Payer              solana.PublicKey
	ProbePK            solana.PublicKey
	PublicIP           *[4]uint8
	LocationOffsetPort *uint16
	LatencyThresholdNs *uint64
	MetricsPublisherPK *solana.PublicKey
}

func (c *UpdateGeoProbeInstructionConfig) Validate() error {
	if c.Payer.IsZero() {
		return fmt.Errorf("payer public key is required")
	}
	if c.ProbePK.IsZero() {
		return fmt.Errorf("probe public key is required")
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
		PublicIP           *[4]uint8        `borsh_optional:"true"`
		LocationOffsetPort *uint16          `borsh_optional:"true"`
		LatencyThresholdNs *uint64          `borsh_optional:"true"`
		MetricsPublisherPK *solana.PublicKey `borsh_optional:"true"`
	}{
		Discriminator:      uint8(UpdateGeoProbeInstructionIndex),
		PublicIP:           config.PublicIP,
		LocationOffsetPort: config.LocationOffsetPort,
		LatencyThresholdNs: config.LatencyThresholdNs,
		MetricsPublisherPK: config.MetricsPublisherPK,
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
