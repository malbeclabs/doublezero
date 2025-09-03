package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/metrics"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

const (
	defaultMaxAttempts = 5
)

type SubmitterConfig struct {
	Interval           time.Duration
	Buffer             buffer.PartitionedBuffer[PartitionKey, Sample]
	MetricsPublisherPK solana.PublicKey
	ProbeInterval      time.Duration
	ProgramClient      TelemetryProgramClient
	BackoffFunc        func(attempt int) time.Duration // optional, defaults to exponential backoff
	MaxAttempts        int                             // optional, defaults to 5
	GetCurrentEpoch    func(ctx context.Context) (uint64, error)
}

// Submitter periodically flushes collected telemetry samples from the sample
// buffer and submits them to the on-chain telemetry program. It includes retry
// logic with jittered exponential backoff for robustness.
type Submitter struct {
	log *slog.Logger
	cfg *SubmitterConfig
	rng *rand.Rand
}

func NewSubmitter(log *slog.Logger, cfg *SubmitterConfig) (*Submitter, error) {
	if cfg.GetCurrentEpoch == nil {
		return nil, fmt.Errorf("GetCurrentEpoch is required")
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = defaultMaxAttempts
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return &Submitter{
		log: log,
		cfg: cfg,
		rng: rng,
	}, nil
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
			s.Tick(ctx)
		}
	}
}

func (s *Submitter) SubmitSamples(ctx context.Context, partitionKey PartitionKey, samples []Sample) error {
	log := s.log.With("partition", partitionKey)

	if len(samples) == 0 {
		log.Debug("No samples to submit, skipping")
		return nil
	}

	for i := 0; i < len(samples); i += telemetry.MaxSamplesPerBatch {
		end := min(i+telemetry.MaxSamplesPerBatch, len(samples))
		batch := samples[i:end]

		rtts := make([]uint32, len(batch))
		var minTimestamp time.Time
		for j, sample := range batch {
			if sample.Loss {
				rtts[j] = 0
			} else {
				if sample.RTT == 0 {
					// If the RTT is 0 but it was not a loss, we assume it's a spurious negative RTT
					// and set it to 1 microsecond to avoid representing it as a loss in the telemetry
					// program samples, which is what 0 means there.
					rtts[j] = 1
				} else {
					rtts[j] = uint32(sample.RTT.Microseconds())
				}
			}
			if minTimestamp.IsZero() || sample.Timestamp.Before(minTimestamp) {
				minTimestamp = sample.Timestamp
			}
		}

		writeConfig := telemetry.WriteDeviceLatencySamplesInstructionConfig{
			AgentPK:                    s.cfg.MetricsPublisherPK,
			OriginDevicePK:             partitionKey.OriginDevicePK,
			TargetDevicePK:             partitionKey.TargetDevicePK,
			LinkPK:                     partitionKey.LinkPK,
			Epoch:                      &partitionKey.Epoch,
			StartTimestampMicroseconds: uint64(minTimestamp.UnixMicro()),
			Samples:                    rtts,
		}

		_, _, err := s.cfg.ProgramClient.WriteDeviceLatencySamples(ctx, writeConfig)
		if err != nil {
			if errors.Is(err, telemetry.ErrAccountNotFound) {
				log.Info("Account not found, initializing new account")
				_, _, err = s.cfg.ProgramClient.InitializeDeviceLatencySamples(ctx, telemetry.InitializeDeviceLatencySamplesInstructionConfig{
					AgentPK:                      s.cfg.MetricsPublisherPK,
					OriginDevicePK:               partitionKey.OriginDevicePK,
					TargetDevicePK:               partitionKey.TargetDevicePK,
					LinkPK:                       partitionKey.LinkPK,
					Epoch:                        &partitionKey.Epoch,
					SamplingIntervalMicroseconds: uint64(s.cfg.ProbeInterval.Microseconds()),
				})
				if err != nil {
					metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterFailedToInitializeAccount).Inc()
					return fmt.Errorf("failed to initialize device latency samples: %w", err)
				}
				_, _, err = s.cfg.ProgramClient.WriteDeviceLatencySamples(ctx, writeConfig)
				if err != nil {
					if errors.Is(err, telemetry.ErrSamplesAccountFull) {
						log.Warn("Partition account is full, dropping samples from buffer and moving on", "droppedSamples", len(samples))
						s.cfg.Buffer.Remove(partitionKey)
						return nil
					}
					metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterFailedToWriteSamples).Inc()
					return fmt.Errorf("failed to write device latency samples after init: %w", err)
				}
			} else if errors.Is(err, telemetry.ErrSamplesAccountFull) {
				log.Warn("Partition account is full, dropping samples from buffer and moving on", "droppedSamples", len(samples))
				s.cfg.Buffer.Remove(partitionKey)
				return nil
			} else {
				metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterFailedToWriteSamples).Inc()
				return fmt.Errorf("failed to write device latency samples: %w", err)
			}
		}

		log.Debug("Submitted account samples batch", "count", len(samples), "samples", rtts)
	}

	return nil
}

func (s *Submitter) Tick(ctx context.Context) {
	for partitionKey := range s.cfg.Buffer.FlushWithoutReset() {
		tmp := s.cfg.Buffer.CopyAndReset(partitionKey)

		log := s.log.With("partition", partitionKey)

		log.Debug("Submitting samples", "count", len(tmp))

		if len(tmp) == 0 {
			log.Debug("No samples to submit, skipping")
			s.cfg.Buffer.Recycle(partitionKey, tmp)

			// If the account is for a past epoch, remove it.
			epoch, err := s.getCurrentEpoch(ctx)
			if err != nil {
				log.Error("failed to get current epoch", "error", err)
				continue
			}
			if partitionKey.Epoch < epoch {
				s.cfg.Buffer.Remove(partitionKey)
				log.Debug("Removed account key")
			}
			continue
		}

		success := false
		for attempt := 1; attempt <= s.cfg.MaxAttempts; attempt++ {
			err := s.SubmitSamples(ctx, partitionKey, tmp)
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
			case s.cfg.MaxAttempts:
				metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterRetriesExhausted).Inc()
				log.Error("Submission failed after all retries", "attempt", attempt, "samplesCount", len(tmp), "error", err)
			case (s.cfg.MaxAttempts + 1) / 2:
				log.Debug("Submission failed, still retrying...", "attempt", attempt, "error", err)
			default:
				log.Debug("Submission failed, retrying...", "attempt", attempt, "delay", backoff, "error", err)
			}

			if !sleepOrDone(ctx, backoff) {
				log.Debug("Submission retry aborted by context")
				break
			}
		}

		// If submission failed and the buffer is not at capacity, prepend the samples back to the
		// buffer. If the buffer is at capacity and we have failed all attempts, don't prepend the
		// samples back to the buffer.
		overCapacity := s.cfg.Buffer.Len(partitionKey)+len(tmp) >= s.cfg.Buffer.Capacity(partitionKey)
		if !success && !overCapacity {
			s.cfg.Buffer.PriorityPrepend(partitionKey, tmp)
		}

		// Always recycle the slice for reuse
		s.cfg.Buffer.Recycle(partitionKey, tmp)
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

// getCurrentEpoch gets the current epoch, with a few retries to mitigate any transient network
// issues. The submitter does not rely on this to succeed, and will just try again on the next tick
// if it fails all retries.
func (s *Submitter) getCurrentEpoch(ctx context.Context) (uint64, error) {
	attempt := 0
	epoch, err := backoff.Retry(ctx, func() (uint64, error) {
		if attempt > 1 {
			s.log.Warn("Failed to get current epoch, retrying", "attempt", attempt)
		}
		attempt++
		epoch, err := s.cfg.GetCurrentEpoch(ctx)
		if err != nil {
			return 0, err
		}
		return epoch, nil
	}, backoff.WithBackOff(backoff.NewExponentialBackOff()), backoff.WithMaxTries(5))
	if err != nil {
		return 0, fmt.Errorf("failed to get current epoch: %w", err)
	}
	return epoch, nil
}
