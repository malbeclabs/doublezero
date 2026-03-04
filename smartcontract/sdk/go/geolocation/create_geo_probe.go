package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type CreateGeoProbeInstructionConfig struct {
	Payer                       solana.PublicKey
	Code                        string
	ExchangePK                  solana.PublicKey
	ServiceabilityGlobalStatePK solana.PublicKey
	PublicIP                    [4]uint8
	LocationOffsetPort          uint16
	LatencyThresholdNs          uint64
	MetricsPublisherPK          solana.PublicKey
}

func (c *CreateGeoProbeInstructionConfig) Validate() error {
	if c.Payer.IsZero() {
		return fmt.Errorf("payer public key is required")
	}
	if c.Code == "" {
		return fmt.Errorf("code is required")
	}
	if len(c.Code) > MaxCodeLength {
		return fmt.Errorf("code length %d exceeds max %d", len(c.Code), MaxCodeLength)
	}
	if c.ExchangePK.IsZero() {
		return fmt.Errorf("exchange public key is required")
	}
	if c.ServiceabilityGlobalStatePK.IsZero() {
		return fmt.Errorf("serviceability global state public key is required")
	}
	if c.MetricsPublisherPK.IsZero() {
		return fmt.Errorf("metrics publisher public key is required")
	}
	return nil
}

func BuildCreateGeoProbeInstruction(
	programID solana.PublicKey,
	config CreateGeoProbeInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	data, err := borsh.Serialize(struct {
		Discriminator      uint8
		Code               string
		PublicIP           [4]uint8
		LocationOffsetPort uint16
		LatencyThresholdNs uint64
		MetricsPublisherPK solana.PublicKey
	}{
		Discriminator:      uint8(CreateGeoProbeInstructionIndex),
		Code:               config.Code,
		PublicIP:           config.PublicIP,
		LocationOffsetPort: config.LocationOffsetPort,
		LatencyThresholdNs: config.LatencyThresholdNs,
		MetricsPublisherPK: config.MetricsPublisherPK,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	probePDA, _, err := DeriveGeoProbePDA(programID, config.Code)
	if err != nil {
		return nil, fmt.Errorf("failed to derive geo probe PDA: %w", err)
	}

	programConfigPDA, _, err := DeriveProgramConfigPDA(programID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive program config PDA: %w", err)
	}

	accounts := []*solana.AccountMeta{
		{PublicKey: probePDA, IsSigner: false, IsWritable: true},
		{PublicKey: config.ExchangePK, IsSigner: false, IsWritable: false},
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
