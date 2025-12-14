package gm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/metrics"
)

type TargetSetConfig struct {
	Clock clockwork.Clock

	ProbeTimeout     time.Duration
	MaxConcurrency   int
	VerboseFailures  bool
	VerboseSuccesses bool
}

func (c *TargetSetConfig) Validate() error {
	if c.Clock == nil {
		return errors.New("clock is required")
	}
	if c.ProbeTimeout <= 0 {
		return errors.New("probe timeout must be greater than 0")
	}
	if c.MaxConcurrency <= 0 {
		return errors.New("max concurrency must be greater than 0")
	}
	return nil
}

type TargetSet struct {
	log *slog.Logger
	cfg *TargetSetConfig

	byID map[ProbeTargetID]ProbeTarget
	mu   sync.Mutex
}

func NewTargetSet(log *slog.Logger, cfg *TargetSetConfig) (*TargetSet, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid target set config: %w", err)
	}
	return &TargetSet{
		log: log,
		cfg: cfg,

		byID: make(map[ProbeTargetID]ProbeTarget),
	}, nil
}

func (s *TargetSet) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byID)
}

// Prune the target set by closing any targets that are not in the given map.
// Pruning a target will close it and remove it from the target set.
func (s *TargetSet) Prune(targets map[ProbeTargetID]ProbeTarget) {
	if targets == nil {
		targets = map[ProbeTargetID]ProbeTarget{}
	}

	// Copy to avoid holding on to caller-owned map.
	newByID := make(map[ProbeTargetID]ProbeTarget, len(targets))
	maps.Copy(newByID, targets)

	s.pruneOwned(newByID)
}

func (s *TargetSet) pruneOwned(targets map[ProbeTargetID]ProbeTarget) {
	targetsToClose := make(map[ProbeTargetID]ProbeTarget, len(s.byID))

	s.mu.Lock()
	for id, oldTarget := range s.byID {
		if _, ok := targets[id]; !ok {
			targetsToClose[id] = oldTarget
		}
	}
	s.byID = targets
	s.mu.Unlock()

	for id := range targetsToClose {
		probeType, path := metricsLabelsFromTargetID(id)
		metrics.TargetsPrunedTotal.WithLabelValues(path, probeType).Inc()
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, s.cfg.MaxConcurrency)
	for _, target := range targetsToClose {
		wg.Add(1)
		sem <- struct{}{}
		go func(target ProbeTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			target.Close()
		}(target)
	}
	wg.Wait()
	close(sem)
}

// Update the target set with the given targets, pruning any targets that are not in the given map.
// Pruning a target will close it and remove it from the target set.
func (s *TargetSet) Update(targets map[ProbeTargetID]ProbeTarget) {
	if targets == nil {
		targets = map[ProbeTargetID]ProbeTarget{}
	}

	// Copy so we don't mutate caller map.
	newTargets := make(map[ProbeTargetID]ProbeTarget, len(targets))
	maps.Copy(newTargets, targets)

	s.mu.Lock()
	for id := range newTargets {
		if existing, ok := s.byID[id]; ok {
			newTargets[id] = existing
		}
	}
	s.mu.Unlock()

	s.Prune(newTargets)
}

func (s *TargetSet) ExecuteProbes(ctx context.Context) (map[ProbeTargetID]*ProbeResult, error) {
	s.mu.Lock()
	targets := make([]ProbeTarget, 0, len(s.byID))
	for _, t := range s.byID {
		targets = append(targets, t)
	}
	s.mu.Unlock()

	results := make(map[ProbeTargetID]*ProbeResult)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.cfg.MaxConcurrency)
	for _, target := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(target ProbeTarget) {
			defer wg.Done()
			defer func() { <-sem }()

			probeType, path := metricsLabelsFromTarget(target)
			metrics.ProbesInflight.WithLabelValues(path, probeType).Inc()
			start := time.Now()
			defer func() {
				metrics.ProbesInflight.WithLabelValues(path, probeType).Dec()
				metrics.ProbeDurations.WithLabelValues(path, probeType).
					Observe(time.Since(start).Seconds())
			}()

			timeoutCtx, cancel := context.WithTimeout(ctx, s.cfg.ProbeTimeout)
			defer cancel()
			now := s.cfg.Clock.Now()
			result, err := target.Probe(timeoutCtx)
			if err != nil {
				switch {
				case errors.Is(err, context.DeadlineExceeded):
					result = &ProbeResult{
						Timestamp:  now,
						FailReason: ProbeFailReasonTimeout,
						FailError:  err,
					}
				case errors.Is(err, context.Canceled):
					return
				default:
					s.log.Error("targets: error executing probe", "target", target.ID(), "error", err)
					return
				}
			}

			if result == nil {
				return
			}

			result.Timestamp = now

			switch result.FailReason {
			case "":
				if s.cfg.VerboseSuccesses && result.Stats != nil {
					s.log.Debug("targets: probe succeeded", "target", target.ID(), "stats", result.Stats.String())
				}
			case ProbeFailReasonNotReady:
				if s.cfg.VerboseFailures && result.Stats != nil {
					s.log.Debug("targets: probe not ready", "target", target.ID(), "reason", result.FailReason, "stats", result.Stats.String())
				}
			case ProbeFailReasonNoRoute:
				// No need to log this.
			default:
				if s.cfg.VerboseFailures {
					if errors.Is(result.FailError, context.DeadlineExceeded) {
						s.log.Debug("targets: probe failed", "target", target.ID(), "reason", result.FailReason)
					} else {
						s.log.Debug("targets: probe failed", "target", target.ID(), "reason", result.FailReason, "error", result.FailError)
					}
				}
			}

			mu.Lock()
			results[target.ID()] = result
			mu.Unlock()
		}(target)
	}
	wg.Wait()
	close(sem)

	return results, nil
}
