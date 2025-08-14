package telemetry

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type InitializeInternetLatencySamplesInstructionConfig struct {
	OriginExchangePK             solana.PublicKey
	TargetExchangePK             solana.PublicKey
	DataProviderName             string
	Epoch                        uint64
	SamplingIntervalMicroseconds uint64
}

func (c *InitializeInternetLatencySamplesInstructionConfig) Validate() error {
	if c.OriginExchangePK.IsZero() {
		return fmt.Errorf("origin location public key is required")
	}
	if c.TargetExchangePK.IsZero() {
		return fmt.Errorf("target location public key is required")
	}
	if c.DataProviderName == "" {
		return fmt.Errorf("data provider name is required")
	}
	if len(c.DataProviderName) > MaxInternetLatencyDataProviderNameLength {
		return fmt.Errorf("data provider name is too long, max length is %d", MaxInternetLatencyDataProviderNameLength)
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
	signerPK solana.PublicKey,
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
		signerPK,
		config.DataProviderName,
		config.OriginExchangePK,
		config.TargetExchangePK,
		config.Epoch,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive PDA: %w", err)
	}

	// Build accounts
	accounts := []*solana.AccountMeta{
		{PublicKey: pda, IsSigner: false, IsWritable: true},
		{PublicKey: signerPK, IsSigner: true, IsWritable: true},
		{PublicKey: config.OriginExchangePK, IsSigner: false, IsWritable: false},
		{PublicKey: config.TargetExchangePK, IsSigner: false, IsWritable: false},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
