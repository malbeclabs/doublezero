package gmon

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

const (
	defaultResultsBuffer  = 1024
	defaultMaxConcurrency = 32
)

type SchedulerConfig struct {
	Logger        *slog.Logger
	Source        TargetSource
	ProbeInterval time.Duration

	ResultsBuffer  int           // buffered results; default 1024
	MaxConcurrency int           // max in-flight probes across all targets; default 32
	ProbeTimeout   time.Duration // per-probe timeout; 0 = no timeout
}

func (c *SchedulerConfig) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Source == nil {
		return errors.New("target source is required")
	}
	if c.ProbeInterval <= 0 {
		return errors.New("probe interval must be greater than 0")
	}
	if c.ResultsBuffer <= 0 {
		c.ResultsBuffer = defaultResultsBuffer
	}
	if c.MaxConcurrency <= 0 {
		c.MaxConcurrency = defaultMaxConcurrency
	}
	return nil
}

type targetRunner struct {
	target Target
	cancel context.CancelFunc
}

type Scheduler struct {
	log *slog.Logger
	cfg *SchedulerConfig

	mu      sync.Mutex
	runners map[TargetID]*targetRunner

	results chan ProbeResult
}

func NewScheduler(cfg SchedulerConfig) (*Scheduler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Scheduler{
		log:     cfg.Logger,
		cfg:     &cfg,
		runners: make(map[TargetID]*targetRunner),
		results: make(chan ProbeResult, cfg.ResultsBuffer),
	}, nil
}

func (s *Scheduler) Results() <-chan ProbeResult { return s.results }

func (s *Scheduler) Start(ctx context.Context, cancel context.CancelFunc) {
	go func() {
		if err := s.Run(ctx); err != nil {
			s.log.Error("scheduler failed to run", "error", err)
			cancel()
		}
	}()
}

func (s *Scheduler) Run(ctx context.Context) error {
	s.log.Debug("scheduler starting",
		"probeInterval", s.cfg.ProbeInterval,
		"maxConcurrency", s.cfg.MaxConcurrency,
	)

	sem := make(chan struct{}, s.cfg.MaxConcurrency)

	for _, t := range s.cfg.Source.All() {
		s.startRunner(ctx, sem, t)
	}

	addedCh := s.cfg.Source.Added()
	removedCh := s.cfg.Source.Removed()

	for {
		select {
		case <-ctx.Done():
			s.log.Debug("scheduler context done, stopping", "reason", ctx.Err())
			s.stopAllRunners()
			return nil

		case t, ok := <-addedCh:
			if addedCh != nil && ok {
				s.startRunner(ctx, sem, t)
			}

		case id, ok := <-removedCh:
			if removedCh != nil && ok {
				s.stopRunner(id)
			}
		}
	}
}

func (s *Scheduler) startRunner(parentCtx context.Context, sem chan struct{}, t Target) {
	id := t.ID()

	s.mu.Lock()
	if _, exists := s.runners[id]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parentCtx)
	s.runners[id] = &targetRunner{target: t, cancel: cancel}
	s.mu.Unlock()

	go s.runTargetLoop(ctx, sem, t)
}

func (s *Scheduler) stopRunner(id TargetID) {
	s.mu.Lock()
	r, ok := s.runners[id]
	if ok {
		delete(s.runners, id)
	}
	s.mu.Unlock()

	if ok && r.cancel != nil {
		r.cancel()
	}
}

func (s *Scheduler) stopAllRunners() {
	s.mu.Lock()
	runners := s.runners
	s.runners = make(map[TargetID]*targetRunner)
	s.mu.Unlock()

	for _, r := range runners {
		if r.cancel != nil {
			r.cancel()
		}
	}
}

func (s *Scheduler) runTargetLoop(ctx context.Context, sem chan struct{}, t Target) {
	s.probeOnce(ctx, sem, t)

	ticker := time.NewTicker(s.cfg.ProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.probeOnce(ctx, sem, t)
		}
	}
}

func (s *Scheduler) probeOnce(parentCtx context.Context, sem chan struct{}, t Target) {
	// Bound overall concurrency.
	select {
	case sem <- struct{}{}:
		// proceed
	case <-parentCtx.Done():
		return
	}
	defer func() { <-sem }()

	probeCtx := parentCtx
	var cancel context.CancelFunc
	if s.cfg.ProbeTimeout > 0 {
		probeCtx, cancel = context.WithTimeout(parentCtx, s.cfg.ProbeTimeout)
		defer cancel()
	}

	res, err := t.Probe(probeCtx)
	if err != nil {
		if !errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			s.log.Error("failed to probe target", "target", t.ID(), "error", err)
		}
		return
	}

	select {
	case s.results <- res:
	default:
		s.log.Warn("results channel full, dropping result")
	}
}
