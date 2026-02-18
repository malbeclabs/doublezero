package onchain

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

const DefaultCacheTTL = 5 * time.Second

// ProgramDataProvider is the interface for fetching onchain program data.
type ProgramDataProvider interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

// CachingFetcher wraps a ProgramDataProvider and caches the result for the
// configured TTL. Multiple consumers calling GetProgramData within the TTL
// window receive the same cached data, avoiding duplicate RPC calls.
type CachingFetcher struct {
	provider  ProgramDataProvider
	cacheTTL  time.Duration
	mu        sync.Mutex
	cached    *serviceability.ProgramData
	fetchedAt time.Time
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
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.cached != nil && time.Since(f.fetchedAt) < f.cacheTTL {
		return f.cached, nil
	}

	data, err := f.provider.GetProgramData(ctx)
	if err != nil {
		if f.cached != nil {
			slog.Warn("onchain: fetch failed, returning stale cached data", "age", time.Since(f.fetchedAt), "error", err)
			return f.cached, nil
		}
		return nil, err
	}

	f.cached = data
	f.fetchedAt = time.Now()
	return data, nil
}
