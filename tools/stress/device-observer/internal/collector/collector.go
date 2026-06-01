// Package collector defines the interface implemented by every long-running
// data-collection goroutine the observer runs under one errgroup.
package collector

import "context"

type Collector interface {
	Run(ctx context.Context) error
}

// Noop blocks until ctx is canceled. Stub collectors return it from their
// constructors until their real implementations land in later PRs.
type Noop struct{}

func (Noop) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
