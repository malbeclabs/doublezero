package geolocation

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
	GetAccountInfo(ctx context.Context, account solana.PublicKey) (out *solanarpc.GetAccountInfoResult, err error)
	GetProgramAccountsWithOpts(ctx context.Context, publicKey solana.PublicKey, opts *solanarpc.GetProgramAccountsOpts) (out solanarpc.GetProgramAccountsResult, err error)
}
