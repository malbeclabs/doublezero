package solana

import (
	"context"
	"fmt"
	"time"
)

type ProbeConfig struct {
	MonitorConfig                       // monitor config
	Duration              time.Duration // how long to run
	AvailabilityThreshold float64       // e.g. 0.95
}

func (c *ProbeConfig) Validate() error {
	if err := c.MonitorConfig.Validate(); err != nil {
		return err
	}
	if c.Duration <= 0 {
		c.Duration = 2 * time.Minute
	}
	if c.AvailabilityThreshold <= 0 {
		c.AvailabilityThreshold = 0.95
	}
	return nil
}

type ProbeResult struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Summaries  []ValidatorSummary

	Unhealthy      []ValidatorSummary
	NeverAvailable []ValidatorSummary
	BelowThreshold []ValidatorSummary
}

func RunProbe(ctx context.Context, cfg ProbeConfig) (*ProbeResult, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	mon, err := NewMonitor(&cfg.MonitorConfig)
	if err != nil {
		return nil, err
	}

	start := cfg.Clock.Now()
	deadline := start.Add(cfg.Duration)

	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	err = mon.Run(ctx)
	if err != nil && err != context.DeadlineExceeded {
		return nil, fmt.Errorf("failed to run solana monitor: %w", err)
	}

	snap := mon.Snapshot()
	var (
		unhealthy      []ValidatorSummary
		neverAvailable []ValidatorSummary
		belowThreshold []ValidatorSummary
	)

	for _, s := range snap {
		avail := s.WindowAvail
		h := s.Health

		if avail == 0 {
			// Distinguish "never had a success" from "had success but window is empty".
			if h.LastSuccess.IsZero() {
				neverAvailable = append(neverAvailable, s)
				unhealthy = append(unhealthy, s)
			}
			continue
		}

		if avail < cfg.AvailabilityThreshold {
			belowThreshold = append(belowThreshold, s)
			unhealthy = append(unhealthy, s)
		}
	}

	return &ProbeResult{
		StartedAt:      start,
		FinishedAt:     cfg.Clock.Now(),
		Summaries:      snap,
		Unhealthy:      unhealthy,
		NeverAvailable: neverAvailable,
		BelowThreshold: belowThreshold,
	}, nil
}
