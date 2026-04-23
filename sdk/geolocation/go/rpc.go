package geolocation

import (
	"context"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

// RPCClient is an interface for reading accounts from the Solana RPC server.
type RPCClient interface {
	GetAccountInfo(ctx context.Context, account solana.PublicKey) (out *solanarpc.GetAccountInfoResult, err error)
	GetProgramAccountsWithOpts(ctx context.Context, publicKey solana.PublicKey, opts *solanarpc.GetProgramAccountsOpts) (out solanarpc.GetProgramAccountsResult, err error)
}

// ExecutorRPCClient is an interface for write-path RPC operations used by the executor.
type ExecutorRPCClient interface {
	GetLatestBlockhash(ctx context.Context, commitment solanarpc.CommitmentType) (out *solanarpc.GetLatestBlockhashResult, err error)
	SendTransactionWithOpts(ctx context.Context, transaction *solana.Transaction, opts solanarpc.TransactionOpts) (sig solana.Signature, err error)
	GetSignatureStatuses(ctx context.Context, searchTransactionHistory bool, transactionSignatures ...solana.Signature) (out *solanarpc.GetSignatureStatusesResult, err error)
	GetTransaction(ctx context.Context, txSig solana.Signature, opts *solanarpc.GetTransactionOpts) (out *solanarpc.GetTransactionResult, err error)
}
