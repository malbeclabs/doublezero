package dzsdk

import (
	"context"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// Interface for sending transactions
type TransactionSender interface {
	SendTransaction(context.Context, *solana.Transaction) (solana.Signature, error)
	GetLatestBlockhash(context.Context, rpc.CommitmentType) (*rpc.GetLatestBlockhashResult, error)
}

// Builds the instruction for initializing DZ latency samples
func BuildInitializeDzLatencySamplesInstruction(
	programID solana.PublicKey,
	telemetryProgramID solana.PublicKey,
	signer solana.PublicKey,
	args *InitializeDzLatencySamplesArgs,
) (solana.Instruction, error) {
	// Derive the PDA
	pda, _, err := DeriveDzLatencySamplesPDA(
		telemetryProgramID,
		args.DeviceAPk,
		args.DeviceZPk,
		args.LinkPk,
		args.Epoch,
	)
	if err != nil {
		return nil, err
	}

	// Serialize the instruction data
	data, err := SerializeInitializeDzLatencySamples(args)
	if err != nil {
		return nil, err
	}

	// Build accounts
	accounts := []*solana.AccountMeta{
		{PublicKey: pda, IsSigner: false, IsWritable: true},
		{PublicKey: signer, IsSigner: true, IsWritable: true},
		{PublicKey: args.DeviceAPk, IsSigner: false, IsWritable: false},
		{PublicKey: args.DeviceZPk, IsSigner: false, IsWritable: false},
		{PublicKey: args.LinkPk, IsSigner: false, IsWritable: false},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
		{PublicKey: programID, IsSigner: false, IsWritable: false}, // serviceability program
	}

	return &solana.GenericInstruction{
		ProgID:        telemetryProgramID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}

// Builds the instruction for writing DZ latency samples
func BuildWriteDzLatencySamplesInstruction(
	telemetryProgramID solana.PublicKey,
	latencySamplesAccount solana.PublicKey,
	signer solana.PublicKey,
	args *WriteDzLatencySamplesArgs,
) (solana.Instruction, error) {
	// Serialize the instruction data
	data, err := SerializeWriteDzLatencySamples(args)
	if err != nil {
		return nil, err
	}

	// Build accounts
	accounts := []*solana.AccountMeta{
		{PublicKey: latencySamplesAccount, IsSigner: false, IsWritable: true},
		{PublicKey: signer, IsSigner: true, IsWritable: false},
	}

	return &solana.GenericInstruction{
		ProgID:        telemetryProgramID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}
