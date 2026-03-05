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
