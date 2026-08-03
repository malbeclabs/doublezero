package serviceability

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"golang.org/x/sync/singleflight"
)

const DefaultCacheTTL = 5 * time.Second

// DefaultRPCTimeout is the maximum time allowed for a single GetProgramData RPC call.
const DefaultRPCTimeout = 30 * time.Second

// ProgramDataProvider is the interface for fetching onchain program data.
type ProgramDataProvider interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

// CachingFetcher wraps a ProgramDataProvider and caches the result for the
// configured TTL. Multiple consumers calling GetProgramData within the TTL
// window receive the same cached data, avoiding duplicate RPC calls.
//
// The mutex is only held briefly to read/write the cache — the RPC call itself
// runs outside the lock via singleflight to avoid blocking concurrent callers.
type CachingFetcher struct {
	log        *slog.Logger
	provider   ProgramDataProvider
	cacheTTL   time.Duration
	rpcTimeout time.Duration
	mu         sync.RWMutex
	cached     *serviceability.ProgramData
	fetchedAt  time.Time
	group      singleflight.Group

	// Stale-serving state, for logging the transitions into and out of it rather than one
	// line per refresh. Guarded by mu.
	staleSince time.Time
	staleReads int
}

// NewCachingFetcher creates a CachingFetcher with the given logger, provider, TTL, and RPC
// timeout.
//
// The logger is injected rather than taken from the package-global slog: the agent builds its
// own handler and never calls slog.SetDefault, so "the ledger is unreachable and I am serving
// stale data" would otherwise be emitted in a different format, without the agent's source
// field, and could not be leveled with the rest of the agent's output.
func NewCachingFetcher(log *slog.Logger, provider ProgramDataProvider, cacheTTL, rpcTimeout time.Duration) *CachingFetcher {
	if log == nil {
		log = slog.Default()
	}
	return &CachingFetcher{
		log:        log,
		provider:   provider,
		cacheTTL:   cacheTTL,
		rpcTimeout: rpcTimeout,
	}
}

// GetProgramData returns cached program data if fresh, otherwise fetches from
// the underlying provider. On fetch error with existing cache, returns stale data.
func (f *CachingFetcher) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	// Fast path: check cache under read lock.
	f.mu.RLock()
	if f.cached != nil && time.Since(f.fetchedAt) < f.cacheTTL {
		data := f.cached
		f.mu.RUnlock()
		return data, nil
	}
	f.mu.RUnlock()

	// Slow path: fetch via singleflight so concurrent callers share one RPC.
	v, err, _ := f.group.Do("fetch", func() (any, error) {
		// Re-check cache — another goroutine may have refreshed it while we waited.
		f.mu.RLock()
		if f.cached != nil && time.Since(f.fetchedAt) < f.cacheTTL {
			data := f.cached
			f.mu.RUnlock()
			return data, nil
		}
		cachedData := f.cached
		cachedAge := time.Since(f.fetchedAt)
		f.mu.RUnlock()

		// Use a detached context so a cancellation from the first caller's ctx
		// does not fail all other waiters sharing this singleflight call.
		// context.WithoutCancel drops both cancellation and deadline, so we
		// add an explicit timeout to bound how long the RPC can block.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), f.rpcTimeout)
		defer cancel()
		start := time.Now()
		data, err := f.provider.GetProgramData(fetchCtx)
		metricFetchDuration.Observe(time.Since(start).Seconds())

		if err != nil {
			if cachedData != nil {
				metricFetchTotal.WithLabelValues(resultErrorStale).Inc()
				metricStaleCacheAge.Set(cachedAge.Seconds())
				f.enterStale(cachedAge, err)
				return cachedData, nil
			}
			metricFetchTotal.WithLabelValues(resultErrorNoCache).Inc()
			return nil, err
		}

		metricFetchTotal.WithLabelValues(resultSuccess).Inc()
		metricStaleCacheAge.Set(0)

		f.mu.Lock()
		f.cached = data
		f.fetchedAt = time.Now()
		staleSince, staleReads := f.staleSince, f.staleReads
		f.staleSince, f.staleReads = time.Time{}, 0
		f.mu.Unlock()

		if !staleSince.IsZero() {
			f.log.Info("Program data fetch recovered, serving fresh data",
				"downtime", time.Since(staleSince), "staleReads", staleReads)
		}

		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*serviceability.ProgramData), nil
}

// enterStale records that a fetch failed and stale data was served, logging only the first
// occurrence. A ledger outage lasts hours and every consumer refresh takes this path, so
// logging each one buries the transition that matters; metricStaleCacheAge carries the
// sustained case and is what alerting should watch.
func (f *CachingFetcher) enterStale(cachedAge time.Duration, err error) {
	f.mu.Lock()
	first := f.staleSince.IsZero()
	if first {
		f.staleSince = time.Now()
	}
	f.staleReads++
	f.mu.Unlock()

	if first {
		f.log.Warn("Program data fetch failed, serving stale cached data", "age", cachedAge, "error", err)
	}
}
