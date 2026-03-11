package telemetry

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type WriteInternetLatencySamplesInstructionConfig struct {
	OriginExchangePK           solana.PublicKey
	TargetExchangePK           solana.PublicKey
	DataProviderName           string
	Epoch                      uint64
	StartTimestampMicroseconds uint64
	Samples                    []uint32
	TimestampIndexPK           *solana.PublicKey // optional: if set, appends a timestamp index entry
}

func (c *WriteInternetLatencySamplesInstructionConfig) Validate() error {
	if c.OriginExchangePK.IsZero() {
		return fmt.Errorf("origin exchange public key is required")
	}
	if c.TargetExchangePK.IsZero() {
		return fmt.Errorf("target exchange public key is required")
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
		config.OriginExchangePK,
		config.TargetExchangePK,
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
	if config.TimestampIndexPK != nil {
		accounts = append(accounts, &solana.AccountMeta{
			PublicKey: *config.TimestampIndexPK, IsSigner: false, IsWritable: true,
		})
	}

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
