package telemetry

import (
	"context"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

// RPCClient is an interface for interacting with the Solana RPC server.
type RPCClient interface {
	SendTransaction(context.Context, *solana.Transaction) (solana.Signature, error)
	SendTransactionWithOpts(context.Context, *solana.Transaction, solanarpc.TransactionOpts) (solana.Signature, error)
	GetLatestBlockhash(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error)
	GetSignatureStatuses(ctx context.Context, searchTransactionHistory bool, transactionSignatures ...solana.Signature) (out *solanarpc.GetSignatureStatusesResult, err error)
	GetTransaction(ctx context.Context, txSig solana.Signature, opts *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error)
	GetAccountDataInto(ctx context.Context, account solana.PublicKey, out any) (err error)
	GetMinimumBalanceForRentExemption(ctx context.Context, dataSize uint64, commitment solanarpc.CommitmentType) (lamport uint64, err error)
}
