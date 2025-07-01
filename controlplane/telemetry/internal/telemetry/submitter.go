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
	Interval      time.Duration
	Buffer        *AccountsBuffer
	AgentPK       solana.PublicKey
	ProbeInterval time.Duration
	ProgramClient TelemetryProgramClient
	BackoffFunc   func(attempt int) time.Duration // optional, defaults to exponential backoff
	MaxAttempts   int                             // optional, defaults to 5
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

	for {
		select {
		case <-ctx.Done():
			s.log.Debug("==> Submission loop done")
			return nil
		case <-ticker.C:
			s.log.Debug("==> Submission loop ticked")
			s.Tick(ctx)
		}
	}
}

func (s *Submitter) SubmitSamples(ctx context.Context, accountKey AccountKey, samples []Sample) error {
	log := s.log.With("account", accountKey)

	if len(samples) == 0 {
		log.Debug("==> No samples to submit, skipping")
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
	log.Debug("==> Submitting account samples", "count", len(samples), "samples", rtts)

	// Get earliest timestamp from samples.
	var minTimestamp time.Time
	for _, sample := range samples {
		if minTimestamp.IsZero() || sample.Timestamp.Before(minTimestamp) {
			minTimestamp = sample.Timestamp
		}
	}
	startTimestampMicroseconds := uint64(minTimestamp.UnixMicro())

	writeConfig := telemetry.WriteDeviceLatencySamplesInstructionConfig{
		AgentPK:                    s.cfg.AgentPK,
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
			log.Debug("==> Account not found, initializing")
			_, _, err = s.cfg.ProgramClient.InitializeDeviceLatencySamples(ctx, telemetry.InitializeDeviceLatencySamplesInstructionConfig{
				AgentPK:                      s.cfg.AgentPK,
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

	log.Debug("==> Submitted account samples", "count", len(samples))

	return nil
}

func (s *Submitter) Tick(ctx context.Context) {
	maxAttempts := s.cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}

	for accountKey := range s.cfg.Buffer.FlushWithoutReset() {
		// Copy samples and reset buffer for this account
		samples := s.cfg.Buffer.CopyAndReset(accountKey)

		s.log.Debug("==> Submitting samples", "account", accountKey, "count", len(samples))

		if len(samples) == 0 {
			s.log.Debug("==> No samples to submit, skipping")
			s.cfg.Buffer.Recycle(accountKey, samples)
			continue
		}

		func() {
			defer s.cfg.Buffer.Recycle(accountKey, samples)

			for attempt := 1; attempt <= maxAttempts; attempt++ {
				err := s.SubmitSamples(ctx, accountKey, samples)
				if err == nil {
					s.log.Debug("==> Submitted samples", "count", len(samples), "attempt", attempt)
					break
				}

				if attempt == maxAttempts {
					s.log.Error("==> Failed to submit samples after retries", "error", err, "accountKey", accountKey, "samplesCount", len(samples))
					// Re-add failed samples back to buffer for next tick
					for _, sample := range samples {
						s.cfg.Buffer.Add(accountKey, sample)
					}
					return
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
		}()
	}
}
