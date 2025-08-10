package data

import (
	"context"
	"fmt"
	"time"
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

func (p *provider) GetCachedCircuitLatencies(ctx context.Context, circuitCode string, epoch uint64, dataProvider string) *CircuitLatenciesWithHeader {
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()
	cached := p.cache.Get(circuitLatenciesForEpochCacheKey(circuitCode, epoch, dataProvider))
	if cached == nil {
		return nil
	}
	return cached.Value().(*CircuitLatenciesWithHeader)
}

func (p *provider) SetCachedCircuitLatencies(ctx context.Context, circuitCode string, epoch uint64, dataProvider string, latencies *CircuitLatenciesWithHeader, ttl time.Duration) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	p.cache.Set(circuitLatenciesForEpochCacheKey(circuitCode, epoch, dataProvider), latencies, ttl)
}

func circuitLatenciesForEpochCacheKey(circuitCode string, epoch uint64, dataProvider string) string {
	return fmt.Sprintf("latencies:%s:%d:%s", circuitCode, epoch, dataProvider)
}
