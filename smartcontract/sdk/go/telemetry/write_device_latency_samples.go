package telemetry

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type WriteDeviceLatencySamplesInstructionConfig struct {
	AgentPK                    solana.PublicKey
	OriginDevicePK             solana.PublicKey
	TargetDevicePK             solana.PublicKey
	LinkPK                     solana.PublicKey
	Epoch                      uint64
	StartTimestampMicroseconds uint64
	Samples                    []uint32
}

func (c *WriteDeviceLatencySamplesInstructionConfig) Validate() error {
	if c.AgentPK.IsZero() {
		return fmt.Errorf("agent public key is required")
	}
	if c.OriginDevicePK.IsZero() {
		return fmt.Errorf("origin device public key is required")
	}
	if c.TargetDevicePK.IsZero() {
		return fmt.Errorf("target device public key is required")
	}
	if c.LinkPK.IsZero() {
		return fmt.Errorf("link public key is required")
	}
	if c.Epoch == 0 {
		return fmt.Errorf("epoch is required")
	}
	return nil
}

// Builds the instruction for writing device latency samples.
func BuildWriteDeviceLatencySamplesInstruction(
	programID solana.PublicKey,
	config WriteDeviceLatencySamplesInstructionConfig,
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
		Discriminator:              uint8(WriteDeviceLatencySamplesInstructionIndex),
		StartTimestampMicroseconds: config.StartTimestampMicroseconds,
		Samples:                    config.Samples,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	// Derive the PDA.
	pda, _, err := DeriveDeviceLatencySamplesPDA(
		programID,
		config.OriginDevicePK,
		config.TargetDevicePK,
		config.LinkPK,
		config.Epoch,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive PDA: %w", err)
	}

	// Build accounts.
	accounts := []*solana.AccountMeta{
		{PublicKey: pda, IsSigner: false, IsWritable: true},
		{PublicKey: config.AgentPK, IsSigner: true, IsWritable: false},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
