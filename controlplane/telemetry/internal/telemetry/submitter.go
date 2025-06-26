package telemetry

import (
	"context"
	"log/slog"
	"math/rand"
	"time"
)

type SubmitterConfig struct {
	Interval    time.Duration
	Buffer      *SampleBuffer
	SubmitFunc  func(context.Context, []Sample) error
	BackoffFunc func(attempt int) time.Duration // optional, defaults to exponential backoff
	MaxAttempts int                             // optional, defaults to 5
}

// Submitter periodically flushes collected telemetry samples from the sample
// buffer and submits them to the on-chain telemetry program. It includes retry
// logic with jittered exponential backoff for robustness.
type Submitter struct {
	log *slog.Logger
	cfg *SubmitterConfig
	rng *rand.Rand
}

func NewSubmitter(log *slog.Logger, cfg *SubmitterConfig) *Submitter {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return &Submitter{
		log: log,
		cfg: cfg,
		rng: rng,
	}
}

func (s *Submitter) Run(ctx context.Context) error {
	s.log.Info("==> Starting submission loop")

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	maxAttempts := s.cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}

	for {
		select {
		case <-ctx.Done():
			s.log.Debug("==> Submission loop done")
			return nil
		case <-ticker.C:
			s.log.Debug("==> Submission loop ticked")

			tmp := s.cfg.Buffer.CopyAndReset()
			if len(tmp) == 0 {
				s.log.Debug("==> No samples to submit, skipping")
				s.cfg.Buffer.Recycle(tmp)
				continue
			}

			// NOTE: Use tmp directly and defer recycling
			func() {
				defer s.cfg.Buffer.Recycle(tmp)

				for attempt := 1; attempt <= maxAttempts; attempt++ {
					err := s.cfg.SubmitFunc(ctx, tmp)
					if err == nil {
						s.log.Debug("==> Submitted samples", "count", len(tmp), "attempt", attempt)
						break
					}

					if attempt == maxAttempts {
						s.log.Error("==> Failed to submit samples after retries", "error", err)
						break
					}

					var backoff time.Duration
					if s.cfg.BackoffFunc != nil {
						backoff = s.cfg.BackoffFunc(attempt)
					} else {
						base := 250 * time.Millisecond
						jitter := time.Duration(float64(base) * (0.5 + 0.5*s.rng.Float64()))
						backoff = time.Duration(attempt) * jitter
					}

					s.log.Warn("==> Submission failed, retrying", "attempt", attempt, "delay", backoff, "error", err)

					if !sleepOrDone(ctx, backoff) {
						s.log.Debug("==> Submission retry aborted by context")
						return
					}
				}
			}() // Call closure immediately
		}
	}
}
