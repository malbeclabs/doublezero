package shreds

import (
	"testing"
	"time"
)

// A finalized blockhash is valid for roughly 56s. NewRPCClient must bound each
// request well inside that window, or a request queued behind a slow endpoint can
// outlive its blockhash and surface as a BlockhashNotFound preflight failure.
// Enforcement of the bound itself is tested in tools/solana/pkg/rpc; this pins that
// shreds still passes one.
const blockhashValidity = 56 * time.Second

func TestRPCOptions_RequestBoundedInsideBlockhashWindow(t *testing.T) {
	opts := rpcOptions()

	if opts.RequestTimeout <= 0 {
		t.Fatal("request timeout must be bounded, not the 5-minute default")
	}
	if opts.RequestTimeout >= blockhashValidity {
		t.Fatalf("request timeout %s must be well under the %s blockhash window", opts.RequestTimeout, blockhashValidity)
	}
	if opts.MaxConnsPerHost != defaultMaxConns {
		t.Fatalf("expected MaxConnsPerHost %d, got %d", defaultMaxConns, opts.MaxConnsPerHost)
	}
}
