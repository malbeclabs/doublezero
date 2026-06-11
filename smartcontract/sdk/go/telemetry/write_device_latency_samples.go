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
	Epoch                      *uint64
	StartTimestampMicroseconds uint64
	Samples                    []uint32
	AgentVersion               string
	AgentCommit                string
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
	if c.Epoch == nil {
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
	epoch := *config.Epoch

	// Serialize the instruction data.
	var agentVersion [16]byte
	copy(agentVersion[:], config.AgentVersion)
	var agentCommit [8]byte
	copy(agentCommit[:], config.AgentCommit)

	data, err := borsh.Serialize(struct {
		Discriminator              uint8
		StartTimestampMicroseconds uint64
		Samples                    []uint32
		AgentVersion               [16]byte
		AgentCommit                [8]byte
	}{
		Discriminator:              uint8(WriteDeviceLatencySamplesInstructionIndex),
		StartTimestampMicroseconds: config.StartTimestampMicroseconds,
		Samples:                    config.Samples,
		AgentVersion:               agentVersion,
		AgentCommit:                agentCommit,
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
		epoch,
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
