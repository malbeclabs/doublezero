package telemetry

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type InitializeDeviceLatencySamplesInstructionConfig struct {
	AgentPK                      solana.PublicKey
	OriginDevicePK               solana.PublicKey
	TargetDevicePK               solana.PublicKey
	LinkPK                       solana.PublicKey
	Epoch                        uint64
	SamplingIntervalMicroseconds uint64
}

func (c *InitializeDeviceLatencySamplesInstructionConfig) Validate() error {
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
	if c.SamplingIntervalMicroseconds == 0 {
		return fmt.Errorf("sampling interval microseconds is required")
	}
	return nil
}

func BuildInitializeDeviceLatencySamplesInstruction(
	programID solana.PublicKey,
	config InitializeDeviceLatencySamplesInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	// Serialize the instruction data
	data, err := borsh.Serialize(struct {
		Discriminator                uint8
		Epoch                        uint64
		SamplingIntervalMicroseconds uint64
	}{
		Discriminator:                uint8(InitializeDeviceLatencySamplesInstructionIndex),
		Epoch:                        config.Epoch,
		SamplingIntervalMicroseconds: config.SamplingIntervalMicroseconds,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	// Derive the PDA.
	pda, _, err := DeriveDeviceLatencySamplesAddress(
		config.AgentPK,
		programID,
		config.OriginDevicePK,
		config.TargetDevicePK,
		config.LinkPK,
		config.Epoch,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive PDA: %w", err)
	}

	// Build accounts
	accounts := []*solana.AccountMeta{
		{PublicKey: pda, IsSigner: false, IsWritable: true},
		{PublicKey: config.AgentPK, IsSigner: true, IsWritable: true},
		{PublicKey: config.OriginDevicePK, IsSigner: false, IsWritable: false},
		{PublicKey: config.TargetDevicePK, IsSigner: false, IsWritable: false},
		{PublicKey: config.LinkPK, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
