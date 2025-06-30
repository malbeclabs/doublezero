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

// Builds the instruction for initializing device latency samples
func BuildInitializeDeviceLatencySamplesInstruction(
	serviceabilityProgramID solana.PublicKey,
	telemetryProgramID solana.PublicKey,
	signer solana.PublicKey,
	args *InitializeDeviceLatencySamplesArgs,
) (solana.Instruction, error) {
	// Derive the PDA
	pda, _, err := DeriveDeviceLatencySamplesPDA(
		telemetryProgramID,
		args.OriginDevicePK,
		args.TargetDevicePK,
		args.LinkPK,
		args.Epoch,
	)
	if err != nil {
		return nil, err
	}

	// Serialize the instruction data
	data, err := SerializeInitializeDeviceLatencySamples(args)
	if err != nil {
		return nil, err
	}

	// Build accounts
	accounts := []*solana.AccountMeta{
		{PublicKey: pda, IsSigner: false, IsWritable: true},
		{PublicKey: signer, IsSigner: true, IsWritable: true},
		{PublicKey: args.OriginDevicePK, IsSigner: false, IsWritable: false},
		{PublicKey: args.TargetDevicePK, IsSigner: false, IsWritable: false},
		{PublicKey: args.LinkPK, IsSigner: false, IsWritable: false},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
		{PublicKey: serviceabilityProgramID, IsSigner: false, IsWritable: false}, // serviceability program
	}

	return &solana.GenericInstruction{
		ProgID:        telemetryProgramID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}

// Builds the instruction for writing device latency samples
func BuildWriteDeviceLatencySamplesInstruction(
	telemetryProgramID solana.PublicKey,
	latencySamplesAccount solana.PublicKey,
	signer solana.PublicKey,
	args *WriteDeviceLatencySamplesArgs,
) (solana.Instruction, error) {
	// Serialize the instruction data
	data, err := SerializeWriteDeviceLatencySamples(args)
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
