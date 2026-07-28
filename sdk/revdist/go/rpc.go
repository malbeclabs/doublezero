package revdist

import (
	"github.com/gagliardetto/solana-go/rpc"
	dzrpc "github.com/malbeclabs/doublezero/tools/solana/pkg/rpc"
)

// NewRPCClient creates a Solana RPC client with automatic retry on transient errors.
func NewRPCClient(url string) *rpc.Client {
	return dzrpc.New(url, dzrpc.Options{})
}
