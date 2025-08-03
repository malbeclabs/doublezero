package telemetry

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type WriteInternetLatencySamplesInstructionConfig struct {
	OriginLocationPK           solana.PublicKey
	TargetLocationPK           solana.PublicKey
	DataProviderName           string
	Epoch                      uint64
	StartTimestampMicroseconds uint64
	Samples                    []uint32
}

func (c *WriteInternetLatencySamplesInstructionConfig) Validate() error {
	if c.OriginLocationPK.IsZero() {
		return fmt.Errorf("origin location public key is required")
	}
	if c.TargetLocationPK.IsZero() {
		return fmt.Errorf("target location public key is required")
	}
	if c.DataProviderName == "" {
		return fmt.Errorf("data provider name is required")
	}
	if c.Epoch == 0 {
		return fmt.Errorf("epoch is required")
	}
	return nil
}

// Builds the instruction for writing internet latency samples.
func BuildWriteInternetLatencySamplesInstruction(
	programID solana.PublicKey,
	signerPK solana.PublicKey,
	config WriteInternetLatencySamplesInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	// Serialize the instruction data.
	data, err := borsh.Serialize(struct {
		Discriminator              uint8
		StartTimestampMicroseconds uint64
		Samples                    []uint32
	}{
		Discriminator:              uint8(WriteInternetLatencySamplesInstructionIndex),
		StartTimestampMicroseconds: config.StartTimestampMicroseconds,
		Samples:                    config.Samples,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	// Derive the PDA.
	pda, _, err := DeriveInternetLatencySamplesPDA(
		programID,
		signerPK,
		config.DataProviderName,
		config.OriginLocationPK,
		config.TargetLocationPK,
		config.Epoch,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive PDA: %w", err)
	}

	// Build accounts.
	accounts := []*solana.AccountMeta{
		{PublicKey: pda, IsSigner: false, IsWritable: true},
		{PublicKey: signerPK, IsSigner: true, IsWritable: false},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
