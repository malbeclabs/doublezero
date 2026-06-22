package qa

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

const (
	// Default per-call timeout for a single Solana RPC request before it is
	// treated as a failover-triggering failure.
	defaultRPCTimeout = 30 * time.Second
	// Default backoff budget for the GetUSDCBalance retry loop.
	defaultRPCInitialBackoff = 1 * time.Second
	defaultRPCMaxElapsed     = 30 * time.Second
	defaultRPCMaxRetries     = uint64(5)
	// Default slot-lag threshold (in slots) above which an endpoint is
	// considered stale and skipped in favor of a fresher one. ~150 slots is
	// roughly 60s at 400ms/slot.
	defaultRPCMaxSlotLag = uint64(150)
)

// rpcBudget holds the configurable timeout/retry budget for Solana RPC reads.
// All values are read from the environment with defaults that preserve the
// pre-existing hardcoded behavior, so nothing changes unless CI opts in.
type rpcBudget struct {
	// timeout bounds a single RPC call (per endpoint attempt).
	timeout time.Duration
	// initialBackoff and maxElapsed/maxRetries bound the balance retry loop.
	initialBackoff time.Duration
	maxElapsed     time.Duration
	maxRetries     uint64
	// maxSlotLag is the slot-height lag threshold for active staleness failover.
	maxSlotLag uint64
}

func rpcBudgetFromEnv() rpcBudget {
	return rpcBudget{
		timeout:        envDuration("QA_SOLANA_RPC_TIMEOUT", defaultRPCTimeout),
		initialBackoff: envDuration("QA_SOLANA_RPC_INITIAL_BACKOFF", defaultRPCInitialBackoff),
		maxElapsed:     envDuration("QA_SOLANA_RPC_MAX_ELAPSED", defaultRPCMaxElapsed),
		maxRetries:     envUint("QA_SOLANA_RPC_MAX_RETRIES", defaultRPCMaxRetries),
		maxSlotLag:     envUint("QA_SOLANA_RPC_MAX_SLOT_LAG", defaultRPCMaxSlotLag),
	}
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

func envUint(key string, def uint64) uint64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

// resolveSolanaRPCEndpoints builds the ordered, de-duplicated endpoint list for
// the failover pool: the primary first, then any fallbacks from
// SOLANA_RPC_FALLBACK_URLS (comma-separated). When the fallback var is unset,
// publicDefault is appended as the safety-net endpoint (empty publicDefault adds
// nothing, e.g. on testnet/devnet where the DZ ledger RPC has no public
// mainnet fallback). Blanks are dropped and duplicates collapse so a config
// with no distinct fallback behaves exactly as it does today.
func resolveSolanaRPCEndpoints(primary, publicDefault string) []string {
	var raw []string
	raw = append(raw, primary)
	if fb := os.Getenv("SOLANA_RPC_FALLBACK_URLS"); fb != "" {
		raw = append(raw, strings.Split(fb, ",")...)
	} else if publicDefault != "" {
		raw = append(raw, publicDefault)
	}

	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, u := range raw {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

// solanaRPCPool is a multi-endpoint Solana RPC client with sticky-index
// failover. It implements rpc.JSONRPCClient so failover is transparent to both
// direct rpc.Client reads and the shreds SDK reads (which both build on
// rpc.Client). On a retryable failure (timeout, network error, HTTP 429/5xx)
// the current endpoint is advanced and the next is tried; the last error is
// returned only after every endpoint is exhausted. The sticky index means once
// failed over, the pool stays on the new endpoint rather than bouncing back to
// a known-bad node mid-run.
type solanaRPCPool struct {
	log    *slog.Logger
	urls   []string
	budget rpcBudget

	// clients holds one underlying jsonrpc client per endpoint; endpointRPC
	// wraps each for direct (single-endpoint) probing such as slot-lag checks.
	clients     []jsonrpc.RPCClient
	endpointRPC []*rpc.Client

	mu      sync.Mutex
	current int

	// rpcClient is the lazily-built rpc.Client backed by this failover pool.
	rpcOnce   sync.Once
	rpcClient *rpc.Client
}

// newSolanaRPCPool builds a failover pool over the resolved endpoint list.
// primary is the effective Solana RPC URL; publicDefault is appended as a
// fallback when SOLANA_RPC_FALLBACK_URLS is unset (pass "" to disable, e.g. on
// testnet/devnet). A single resolved endpoint collapses to today's behavior.
func newSolanaRPCPool(log *slog.Logger, primary, publicDefault string, budget rpcBudget) *solanaRPCPool {
	urls := resolveSolanaRPCEndpoints(primary, publicDefault)
	clients := make([]jsonrpc.RPCClient, len(urls))
	endpointRPC := make([]*rpc.Client, len(urls))
	for i, u := range urls {
		clients[i] = jsonrpc.NewClient(u)
		endpointRPC[i] = rpc.NewWithCustomRPCClient(clients[i])
	}
	return &solanaRPCPool{
		log:         log,
		urls:        urls,
		budget:      budget,
		clients:     clients,
		endpointRPC: endpointRPC,
	}
}

// RPC returns the rpc.Client backed by this failover pool. It is safe to pass
// to shreds.New (it satisfies the shreds RPCClient interface).
func (p *solanaRPCPool) RPC() *rpc.Client {
	p.rpcOnce.Do(func() {
		p.rpcClient = rpc.NewWithCustomRPCClient(p)
	})
	return p.rpcClient
}

// CurrentURL returns the URL of the currently-selected endpoint.
func (p *solanaRPCPool) CurrentURL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.urls[p.current]
}

// Failover advances to the next endpoint unconditionally. Used by write paths
// (FeedSeat*) that drive an external command with CurrentURL() and want to
// retry against a different endpoint on failure.
func (p *solanaRPCPool) Failover() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.urls) > 1 {
		p.current = (p.current + 1) % len(p.urls)
	}
}

// EndpointCount returns the number of resolved endpoints.
func (p *solanaRPCPool) EndpointCount() int { return len(p.urls) }

func (p *solanaRPCPool) currentIndex() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.current
}

// advance moves past `from` only if no other caller already advanced the index,
// so concurrent callers that all hit a dead endpoint don't skip healthy ones.
func (p *solanaRPCPool) advance(from int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current == from && len(p.urls) > 1 {
		p.current = (p.current + 1) % len(p.urls)
	}
}

// withFailover runs fn against the current endpoint, failing over to the next
// on a retryable error, until an endpoint succeeds or all are exhausted.
func (p *solanaRPCPool) withFailover(ctx context.Context, fn func(c jsonrpc.RPCClient, callCtx context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt < len(p.clients); attempt++ {
		idx := p.currentIndex()
		callCtx, cancel := context.WithTimeout(ctx, p.budget.timeout)
		err := fn(p.clients[idx], callCtx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		// Don't burn through endpoints if the parent context is done.
		if ctx.Err() != nil {
			return err
		}
		if !isRetryableRPCErr(err) {
			return err
		}
		p.log.Warn("Solana RPC endpoint failed, failing over",
			"endpoint", p.urls[idx], "error", err)
		p.advance(idx)
	}
	return lastErr
}

func (p *solanaRPCPool) CallForInto(ctx context.Context, out any, method string, params []any) error {
	return p.withFailover(ctx, func(c jsonrpc.RPCClient, callCtx context.Context) error {
		return c.CallForInto(callCtx, out, method, params)
	})
}

func (p *solanaRPCPool) CallWithCallback(ctx context.Context, method string, params []any, callback func(*http.Request, *http.Response) error) error {
	return p.withFailover(ctx, func(c jsonrpc.RPCClient, callCtx context.Context) error {
		return c.CallWithCallback(callCtx, method, params, callback)
	})
}

func (p *solanaRPCPool) CallBatch(ctx context.Context, requests jsonrpc.RPCRequests) (jsonrpc.RPCResponses, error) {
	var resp jsonrpc.RPCResponses
	err := p.withFailover(ctx, func(c jsonrpc.RPCClient, callCtx context.Context) error {
		var innerErr error
		resp, innerErr = c.CallBatch(callCtx, requests)
		return innerErr
	})
	return resp, err
}

// SelectHealthiestEndpoint probes every endpoint's slot height and fails over to
// the freshest one if the current endpoint trails the max observed slot by more
// than the configured lag threshold. This turns a "stale but valid" lagging node
// (which never returns an error) into an explicit failover trigger. It is a
// no-op with fewer than two endpoints. Probe failures degrade gracefully: a
// node that can't be probed is treated as maximally lagged (slot 0) and simply
// won't be selected, and if every probe fails the current endpoint is kept.
func (p *solanaRPCPool) SelectHealthiestEndpoint(ctx context.Context) {
	if len(p.clients) < 2 {
		return
	}

	slots := make([]uint64, len(p.clients))
	var maxSlot uint64
	for i := range p.endpointRPC {
		cctx, cancel := context.WithTimeout(ctx, p.budget.timeout)
		slot, err := p.endpointRPC[i].GetSlot(cctx, rpc.CommitmentConfirmed)
		cancel()
		if err != nil {
			p.log.Debug("Solana RPC slot probe failed", "endpoint", p.urls[i], "error", err)
			continue
		}
		slots[i] = slot
		if slot > maxSlot {
			maxSlot = slot
		}
	}
	if maxSlot == 0 {
		// Every probe failed; keep the current endpoint and let error-based
		// failover handle it.
		return
	}

	// Pick the least-lagged (highest-slot) endpoint.
	best := -1
	var bestSlot uint64
	for i, s := range slots {
		if s > bestSlot {
			bestSlot = s
			best = i
		}
	}
	if best < 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	curSlot := slots[p.current]
	if maxSlot-curSlot > p.budget.maxSlotLag {
		p.log.Info("Solana RPC endpoint lagging, failing over to fresher endpoint",
			"from", p.urls[p.current], "fromSlot", curSlot,
			"to", p.urls[best], "toSlot", bestSlot,
			"lag", maxSlot-curSlot, "threshold", p.budget.maxSlotLag)
		p.current = best
	}
}

// isRetryableRPCErr reports whether an RPC error should trigger failover to
// another endpoint: timeouts, network errors, and HTTP 429/5xx. JSON-RPC
// business errors (e.g. invalid params, account-specific errors) and context
// cancellation are NOT retryable — they should surface or be handled by
// poll-until-consistent callers.
func isRetryableRPCErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var httpErr *jsonrpc.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Code == 429 || httpErr.Code >= 500
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	msg := strings.ToLower(err.Error())
	for _, s := range []string{"connection reset", "connection refused", "eof", "timeout", "no such host", "broken pipe"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
