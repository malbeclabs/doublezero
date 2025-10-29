//go:build linux

package probing

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

var (
	// defaultListenBackoff is used when no ListenBackoff is provided.
	defaultListenBackoff = ListenBackoffConfig{
		Initial:    1 * time.Second,
		Max:        30 * time.Second,
		Multiplier: 2,
	}
)

// ListenFunc starts a long-lived listener for probe responses or control-plane
// events. It should block until the context is canceled or an unrecoverable
// error occurs, returning nil on clean shutdown (ctx canceled).
type ListenFunc func(context.Context) error

// ProbeFunc runs a single probe for the given route and returns a ProbeResult.
// It should respect ctx cancellation and timeout (the worker may wrap ctx with
// a per-probe deadline). Returning an error counts as a probe failure unless
// the error is due to worker stop (ctx canceled by parent).
type ProbeFunc func(context.Context, *routing.Route) (ProbeResult, error)

// ListenBackoffConfig controls exponential backoff for ListenFunc retries.
// Multiplier is applied to the previous backoff duration, capped at Max.
type ListenBackoffConfig struct {
	Initial    time.Duration // starting delay before first retry
	Max        time.Duration // upper bound for backoff delay
	Multiplier float64       // growth factor per retry, e.g. 2.0 for doubling
}

// ProbeResult contains probe outcome and basic packet counts.
// OK should reflect end-to-end success criteria for a single probe wave.
type ProbeResult struct {
	OK       bool
	Sent     int
	Received int
}

// Config provides all dependencies and tunables for the probing system.
// Fields marked “Required” must be set; Validate enforces this and applies
// defaults where appropriate.
type Config struct {
	// Required object fields.
	Logger         *slog.Logger      // destination for logs
	Context        context.Context   // root context for worker lifecycle
	Netlink        routing.Netlinker // kernel route interface
	Liveness       LivenessPolicy    // policy for up/down hysteresis
	ListenFunc     ListenFunc        // long-lived listener (with retry/backoff)
	ProbeFunc      ProbeFunc         // per-route probe function
	MaxConcurrency uint              // max concurrent probes per Tick()

	// Required scalar fields.
	Interval      time.Duration       // Tick() period for the worker run loop
	ProbeTimeout  time.Duration       // max duration for a single probe (enforced by worker)
	ListenBackoff ListenBackoffConfig // retry policy for ListenFunc errors; defaulted if zero
}

// Validate verifies required fields and applies defaults for optional fields.
// It returns an error if any required field is missing/invalid.
func (cfg *Config) Validate() error {
	// Required object fields.
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.Context == nil {
		return errors.New("context is required")
	}
	if cfg.Netlink == nil {
		return errors.New("netlink is required")
	}
	if cfg.Liveness == nil {
		return errors.New("liveness policy is required")
	}
	if cfg.ListenFunc == nil {
		return errors.New("listen func is required")
	}
	if cfg.ProbeFunc == nil {
		return errors.New("probe func is required")
	}

	// Required scalar fields.
	if cfg.Interval <= 0 {
		return errors.New("interval is required")
	}
	if cfg.ProbeTimeout <= 0 {
		return errors.New("probe timeout is required")
	}
	if cfg.MaxConcurrency <= 0 {
		return errors.New("max concurrency must be greater than 0")
	}

	// Default values.
	if cfg.ListenBackoff == (ListenBackoffConfig{}) {
		cfg.ListenBackoff = defaultListenBackoff
	}

	return nil
}
