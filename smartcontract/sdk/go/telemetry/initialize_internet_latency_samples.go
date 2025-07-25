package telemetry

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type InitializeInternetLatencySamplesInstructionConfig struct {
	OracleAgentPK                solana.PublicKey
	OriginLocationPK             solana.PublicKey
	TargetLocationPK             solana.PublicKey
	GlobalStatePK                solana.PublicKey
	DataProviderName             string
	Epoch                        uint64
	SamplingIntervalMicroseconds uint64
}

func (c *InitializeInternetLatencySamplesInstructionConfig) Validate() error {
	if c.OracleAgentPK.IsZero() {
		return fmt.Errorf("oracle agent public key is required")
	}
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
	if c.SamplingIntervalMicroseconds == 0 {
		return fmt.Errorf("sampling interval microseconds is required")
	}
	return nil
}

func BuildInitializeInternetLatencySamplesInstruction(
	programID solana.PublicKey,
	config InitializeInternetLatencySamplesInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	// Serialize the instruction data
	data, err := borsh.Serialize(struct {
		Discriminator                uint8
		DataProviderName             string
		Epoch                        uint64
		SamplingIntervalMicroseconds uint64
	}{
		Discriminator:                uint8(InitializeInternetLatencySamplesInstructionIndex),
		DataProviderName:             config.DataProviderName,
		Epoch:                        config.Epoch,
		SamplingIntervalMicroseconds: config.SamplingIntervalMicroseconds,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	// Derive the PDA.
	pda, _, err := DeriveInternetLatencySamplesPDA(
		programID,
		config.DataProviderName,
		config.OriginLocationPK,
		config.TargetLocationPK,
		config.Epoch,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive PDA: %w", err)
	}

	// Build accounts
	accounts := []*solana.AccountMeta{
		{PublicKey: pda, IsSigner: false, IsWritable: true},
		{PublicKey: config.OracleAgentPK, IsSigner: true, IsWritable: true},
		{PublicKey: config.OriginLocationPK, IsSigner: false, IsWritable: false},
		{PublicKey: config.TargetLocationPK, IsSigner: false, IsWritable: false},
		{PublicKey: config.GlobalStatePK, IsSigner: false, IsWritable: false},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
