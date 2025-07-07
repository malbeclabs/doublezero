package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

type SubmitterConfig struct {
	Interval           time.Duration
	Buffer             *AccountsBuffer
	MetricsPublisherPK solana.PublicKey
	ProbeInterval      time.Duration
	ProgramClient      TelemetryProgramClient
	BackoffFunc        func(attempt int) time.Duration // optional, defaults to exponential backoff
	MaxAttempts        int                             // optional, defaults to 5
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
	s.log.Info("Starting submission loop", "interval", s.cfg.Interval, "maxRetries", s.cfg.MaxAttempts, "metricsPublisherPK", s.cfg.MetricsPublisherPK)

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Debug("Submission loop done")
			return nil
		case <-ticker.C:
			s.log.Debug("Submission loop ticked")
			s.Tick(ctx)
		}
	}
}

func (s *Submitter) SubmitSamples(ctx context.Context, accountKey AccountKey, samples []Sample) error {
	log := s.log.With("account", accountKey)

	if len(samples) == 0 {
		log.Debug("No samples to submit, skipping")
		return nil
	}

	rtts := make([]uint32, len(samples))
	for i, sample := range samples {
		if sample.Loss {
			rtts[i] = 0
		} else {
			rtts[i] = uint32(sample.RTT.Microseconds())
		}
	}
	log.Debug("Submitting account samples", "count", len(samples), "samples", rtts)

	// Get earliest timestamp from samples.
	var minTimestamp time.Time
	for _, sample := range samples {
		if minTimestamp.IsZero() || sample.Timestamp.Before(minTimestamp) {
			minTimestamp = sample.Timestamp
		}
	}
	startTimestampMicroseconds := uint64(minTimestamp.UnixMicro())

	writeConfig := telemetry.WriteDeviceLatencySamplesInstructionConfig{
		AgentPK:                    s.cfg.MetricsPublisherPK,
		OriginDevicePK:             accountKey.OriginDevicePK,
		TargetDevicePK:             accountKey.TargetDevicePK,
		LinkPK:                     accountKey.LinkPK,
		Epoch:                      accountKey.Epoch,
		StartTimestampMicroseconds: startTimestampMicroseconds,
		Samples:                    rtts,
	}
	_, _, err := s.cfg.ProgramClient.WriteDeviceLatencySamples(ctx, writeConfig)
	if err != nil {
		if errors.Is(err, telemetry.ErrAccountNotFound) {
			log.Debug("Account not found, initializing")
			_, _, err = s.cfg.ProgramClient.InitializeDeviceLatencySamples(ctx, telemetry.InitializeDeviceLatencySamplesInstructionConfig{
				AgentPK:                      s.cfg.MetricsPublisherPK,
				OriginDevicePK:               accountKey.OriginDevicePK,
				TargetDevicePK:               accountKey.TargetDevicePK,
				LinkPK:                       accountKey.LinkPK,
				Epoch:                        accountKey.Epoch,
				SamplingIntervalMicroseconds: uint64(s.cfg.ProbeInterval.Microseconds()),
			})
			if err != nil {
				return fmt.Errorf("failed to initialize device latency samples: %w", err)
			}

			_, _, err = s.cfg.ProgramClient.WriteDeviceLatencySamples(ctx, writeConfig)
			if err != nil {
				return fmt.Errorf("failed to write device latency samples: %w", err)
			}
		} else {
			return fmt.Errorf("failed to write device latency samples: %w", err)
		}
	}

	log.Debug("Submitted account samples", "count", len(samples))

	return nil
}

func (s *Submitter) Tick(ctx context.Context) {
	maxAttempts := s.cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}

	for accountKey := range s.cfg.Buffer.FlushWithoutReset() {
		tmp := s.cfg.Buffer.CopyAndReset(accountKey)

		log := s.log.With("account", accountKey)

		log.Debug("Submitting samples", "count", len(tmp))

		if len(tmp) == 0 {
			log.Debug("No samples to submit, skipping")
			s.cfg.Buffer.Recycle(accountKey, tmp)

			// If the account is for a past epoch, remove it.
			if accountKey.Epoch < DeriveEpoch(time.Now().UTC()) {
				s.cfg.Buffer.Remove(accountKey)
				log.Debug("Removed account key")
			}
			continue
		}

		success := false
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			err := s.SubmitSamples(ctx, accountKey, tmp)
			if err == nil {
				log.Debug("Submitted samples", "count", len(tmp), "attempt", attempt)
				success = true
				break
			}

			var backoff time.Duration
			if s.cfg.BackoffFunc != nil {
				backoff = s.cfg.BackoffFunc(attempt)
			} else {
				backoff = s.defaultBackoff(attempt)
			}

			switch attempt {
			case 1:
				log.Debug("Submission failed, retrying...", "attempt", attempt, "error", err)
			case maxAttempts:
				log.Error("Submission failed after all retries", "attempt", attempt, "samplesCount", len(tmp), "error", err)
			case (maxAttempts + 1) / 2:
				log.Debug("Submission failed, still retrying...", "attempt", attempt, "error", err)
			default:
				log.Debug("Submission failed, retrying...", "attempt", attempt, "delay", backoff, "error", err)
			}

			if !sleepOrDone(ctx, backoff) {
				log.Debug("Submission retry aborted by context")
				break
			}
		}

		if !success {
			for _, sample := range tmp {
				s.cfg.Buffer.Add(accountKey, sample)
			}
		}

		// Always recycle the slice for reuse
		s.cfg.Buffer.Recycle(accountKey, tmp)
	}
}

func (s *Submitter) defaultBackoff(attempt int) time.Duration {
	base := 500 * time.Millisecond
	max := 5 * time.Second
	jitter := 0.5 + 0.5*s.rng.Float64()
	mult := 1 << uint(attempt-1)
	backoff := time.Duration(float64(base) * float64(mult) * jitter)
	if backoff > max {
		return max
	}
	return backoff
}
