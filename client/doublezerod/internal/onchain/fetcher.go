package onchain

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"golang.org/x/sync/singleflight"
)

const DefaultCacheTTL = 5 * time.Second

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
	provider  ProgramDataProvider
	cacheTTL  time.Duration
	mu        sync.RWMutex
	cached    *serviceability.ProgramData
	fetchedAt time.Time
	group     singleflight.Group
}

// NewCachingFetcher creates a CachingFetcher with the given provider and TTL.
func NewCachingFetcher(provider ProgramDataProvider, cacheTTL time.Duration) *CachingFetcher {
	return &CachingFetcher{
		provider: provider,
		cacheTTL: cacheTTL,
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

		data, err := f.provider.GetProgramData(ctx)
		if err != nil {
			if cachedData != nil {
				slog.Warn("onchain: fetch failed, returning stale cached data", "age", cachedAge, "error", err)
				return cachedData, nil
			}
			return nil, err
		}

		f.mu.Lock()
		f.cached = data
		f.fetchedAt = time.Now()
		f.mu.Unlock()

		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*serviceability.ProgramData), nil
}
