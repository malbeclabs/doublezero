package shreds

import (
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	dzrpc "github.com/malbeclabs/doublezero/tools/solana/pkg/rpc"
)

const (
	// defaultRequestTimeout bounds each individual RPC request. http.DefaultClient has no timeout,
	// so against a slow or degraded RPC endpoint a request can block indefinitely — long enough
	// for a transaction's recent blockhash to expire before it is sent, surfacing as
	// BlockhashNotFound. A short timeout fails fast so the caller can retry with a fresh blockhash.
	defaultRequestTimeout = 15 * time.Second

	// defaultMaxConns caps concurrent connections to the RPC host. http.DefaultClient's transport
	// keeps only 2 idle connections per host, which throttles concurrent callers.
	defaultMaxConns = 128
)

func rpcOptions() dzrpc.Options {
	return dzrpc.Options{
		RequestTimeout:  defaultRequestTimeout,
		MaxConnsPerHost: defaultMaxConns,
	}
}

// NewRPCClient creates a Solana RPC client with a bounded request timeout and automatic retry on
// transient errors.
func NewRPCClient(url string) *rpc.Client {
	return dzrpc.New(url, rpcOptions())
}
