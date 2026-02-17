package serviceability

import (
	"context"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// RPCClient is an interface for interacting with the Solana RPC server.
type RPCClient interface {
	GetProgramAccounts(context.Context, solana.PublicKey) (rpc.GetProgramAccountsResult, error)
	GetAccountInfo(context.Context, solana.PublicKey) (*rpc.GetAccountInfoResult, error)
}
