package data

import (
	"context"
	"fmt"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
)

const (
	circuitsCacheKey = "circuits"
)

func (p *provider) GetCachedCircuits(ctx context.Context) []Circuit {
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()
	cached := p.cache.Get(circuitsCacheKey)
	if cached == nil {
		return nil
	}
	return cached.Value().([]Circuit)
}

func (p *provider) SetCachedCircuits(ctx context.Context, circuits []Circuit) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	p.cache.Set(circuitsCacheKey, circuits, p.cfg.CircuitsCacheTTL)
}

func (p *provider) GetCachedCircuitLatencies(ctx context.Context, circuitCode string, epoch uint64) []stats.CircuitLatencySample {
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()
	cached := p.cache.Get(circuitLatenciesForEpochCacheKey(circuitCode, epoch))
	if cached == nil {
		return nil
	}
	return cached.Value().([]stats.CircuitLatencySample)
}

func (p *provider) SetCachedCircuitLatencies(ctx context.Context, circuitCode string, epoch uint64, latencies []stats.CircuitLatencySample, ttl time.Duration) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	p.cache.Set(circuitLatenciesForEpochCacheKey(circuitCode, epoch), latencies, ttl)
}

func circuitLatenciesForEpochCacheKey(circuitCode string, epoch uint64) string {
	return fmt.Sprintf("latencies:%s:%d", circuitCode, epoch)
}
