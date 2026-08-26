package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/metrics"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

const (
	defaultMaxAttempts                  = 5
	defaultOnSubmitterCloseFlushTimeout = 30 * time.Second
)

type SubmitterConfig struct {
	Interval           time.Duration
	Buffer             buffer.PartitionedBuffer[PartitionKey, Sample]
	MetricsPublisherPK solana.PublicKey
	ProbeInterval      time.Duration
	ProgramClient      TelemetryProgramClient
	BackoffFunc        func(attempt int) time.Duration // optional, defaults to exponential backoff
	MaxAttempts        int                             // optional, defaults to 5
	MaxConcurrency     int
	GetCurrentEpoch    func(ctx context.Context) (uint64, error)
	AgentVersion       string
	AgentCommit        string
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
	if cfg.MaxConcurrency <= 0 {
		return nil, fmt.Errorf("max concurrency must be greater than 0")
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
			s.log.Debug("Submission loop done, flushing remaining samples")
			// Pass a new context since the current one has already been cancelled.
			flushCtx, cancel := context.WithTimeout(context.Background(), defaultOnSubmitterCloseFlushTimeout)
			defer cancel()
			s.Tick(flushCtx)
			s.log.Debug("Flushed remaining samples")
			return nil
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

// SubmitSamples writes samples to the partition's onchain account in batches, and returns how many
// of them were written. That count is the caller's resume point: the batches before it are already
// onchain, so a retry must pass samples[written:] rather than re-sending the whole slice, or those
// samples are appended a second time.
func (s *Submitter) SubmitSamples(ctx context.Context, partitionKey PartitionKey, samples []Sample) (int, error) {
	log := s.log.With("partition", partitionKey)

	if len(samples) == 0 {
		log.Debug("No samples to submit, skipping")
		return 0, nil
	}

	for i := 0; i < len(samples); i += telemetry.MaxDeviceLatencySamplesPerBatch {
		end := min(i+telemetry.MaxDeviceLatencySamplesPerBatch, len(samples))
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
			AgentVersion:               s.cfg.AgentVersion,
			AgentCommit:                s.cfg.AgentCommit,
		}

		_, _, err := s.cfg.ProgramClient.WriteDeviceLatencySamples(ctx, writeConfig)
		if err != nil {
			if errors.Is(err, telemetry.ErrAccountNotFound) {
				log.Info("Account not found, initializing new account")
				_, _, initErr := s.cfg.ProgramClient.InitializeDeviceLatencySamples(ctx, telemetry.InitializeDeviceLatencySamplesInstructionConfig{
					AgentPK:                      s.cfg.MetricsPublisherPK,
					OriginDevicePK:               partitionKey.OriginDevicePK,
					TargetDevicePK:               partitionKey.TargetDevicePK,
					LinkPK:                       partitionKey.LinkPK,
					Epoch:                        &partitionKey.Epoch,
					SamplingIntervalMicroseconds: uint64(s.cfg.ProbeInterval.Microseconds()),
					AgentVersion:                 s.cfg.AgentVersion,
					AgentCommit:                  s.cfg.AgentCommit,
				})
				if initErr != nil {
					// Not fatal on its own. An init the program rejects because the account already
					// exists has left us with exactly what the write needs, which happens when a
					// previous init landed onchain without the agent seeing it succeed. The write
					// below is what decides whether the rejection mattered, so it runs either way.
					// The error counter waits for that verdict rather than firing on a failure the
					// write goes on to absorb.
					log.Warn("Failed to initialize account, attempting the write anyway", "error", initErr)
				}
				_, _, err = s.cfg.ProgramClient.WriteDeviceLatencySamples(ctx, writeConfig)
				if err != nil {
					if errors.Is(err, telemetry.ErrSamplesAccountFull) {
						s.handleAccountFull(log, partitionKey, len(samples)-i)
						return i, nil
					}
					if initErr != nil {
						// The account is still not there, so the init failure is the reason the
						// write had nothing to write to. Report that rather than the missing
						// account it caused.
						metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterFailedToInitializeAccount).Inc()
						return i, fmt.Errorf("failed to initialize device latency samples: %w", initErr)
					}
					metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterFailedToWriteSamples).Inc()
					return i, fmt.Errorf("failed to write device latency samples after init: %w", err)
				}
			} else if errors.Is(err, telemetry.ErrSamplesAccountFull) {
				s.handleAccountFull(log, partitionKey, len(samples)-i)
				return i, nil
			} else {
				metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterFailedToWriteSamples).Inc()
				return i, fmt.Errorf("failed to write device latency samples: %w", err)
			}
		}

		log.Debug("Submitted account samples batch", "count", len(batch), "samples", rtts)
	}

	return len(samples), nil
}

// handleAccountFull records the samples lost when a partition's onchain account can no longer
// accept writes, and drops the partition.
//
// Everything that has not been written is gone: SubmitSamples returns without attempting the
// batches after the one that failed, the caller treats the partition as submitted and does not
// requeue, and removing the partition discards whatever the collector buffered since the flush.
// unsubmitted counts the failing batch plus every batch behind it; buffered is read before the
// removal. Batches an earlier attempt already wrote are excluded, since Tick resumes each retry at
// the first unwritten sample.
func (s *Submitter) handleAccountFull(log *slog.Logger, partitionKey PartitionKey, unsubmitted int) {
	buffered := s.cfg.Buffer.Len(partitionKey)

	metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterAccountFull).Inc()
	metrics.SamplesDropped.WithLabelValues(metrics.DropReasonAccountFull).Add(float64(unsubmitted + buffered))
	log.Warn("Partition account is full, dropping partition",
		"unsubmittedSamples", unsubmitted,
		"bufferedSamples", buffered)

	s.cfg.Buffer.Remove(partitionKey)
}

func (s *Submitter) Tick(ctx context.Context) {
	partitions := s.cfg.Buffer.FlushWithoutReset()
	if len(partitions) == 0 {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.cfg.MaxConcurrency)
	wg.Add(len(partitions))
	for partitionKey := range partitions {
		go func(partitionKey PartitionKey) {
			defer wg.Done()
			defer func() { <-sem }() // limit concurrency
			sem <- struct{}{}

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
					return
				}
				if partitionKey.Epoch < epoch {
					s.cfg.Buffer.Remove(partitionKey)
					log.Debug("Removed account key")
				}
				return
			}

			// Samples written so far across attempts. Each retry resumes here rather than at the
			// start of tmp, so batches an earlier attempt put onchain are neither re-sent nor
			// counted as lost.
			written := 0

			success := false
			for attempt := 1; attempt <= s.cfg.MaxAttempts; attempt++ {
				n, err := s.SubmitSamples(ctx, partitionKey, tmp[written:])
				written += n
				if err == nil {
					log.Debug("Submitted samples", "count", len(tmp), "attempt", attempt)
					success = true
					break
				}

				// A rejection by the program is not transient: the ledger executed the instruction
				// and refused it, so every attempt this tick would be refused the same way. Report
				// it once at Error and leave the rest of the attempts unspent, rather than burying
				// the reason under a backoff loop. The samples are requeued as with any other
				// failure, so the next tick retries once the operator fixes what was wrong.
				var programErr *telemetry.ProgramError
				if errors.As(err, &programErr) {
					metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterProgramError).Inc()
					log.Error("Submission rejected by the telemetry program, not retrying this tick",
						"attempt", attempt, "samplesCount", len(tmp), "error", err)
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

			// If submission failed and the buffer is not at capacity, prepend the samples that were
			// never written back to the buffer. If the buffer is at capacity and we have failed all
			// attempts, they are discarded; log and count the loss.
			if !success {
				unwritten := tmp[written:]
				capacity := s.cfg.Buffer.Capacity(partitionKey)
				bufLen := s.cfg.Buffer.Len(partitionKey)

				// room is how many of the unwritten samples still fit without exceeding capacity.
				// Keep the newest `room` of them (the tail: batches are submitted oldest-first, so
				// unwritten is already in chronological order) and drop only the rest, rather than
				// discarding the whole slice the moment it stops fitting entirely.
				room := max(capacity-bufLen, 0)
				kept := unwritten
				if len(unwritten) > room {
					kept = unwritten[len(unwritten)-room:]
				}

				if dropped := len(unwritten) - len(kept); dropped > 0 {
					metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterBufferFull).Inc()
					metrics.SamplesDropped.WithLabelValues(metrics.DropReasonBufferFull).Add(float64(dropped))
					log.Warn("Partition buffer at capacity after failed submission, dropping oldest samples",
						"droppedSamples", dropped,
						"keptSamples", len(kept),
						"bufferLen", bufLen,
						"capacity", capacity)
				}
				if len(kept) > 0 {
					s.cfg.Buffer.PriorityPrepend(partitionKey, kept)
				}
			}

			// Always recycle the slice for reuse
			s.cfg.Buffer.Recycle(partitionKey, tmp)
		}(partitionKey)
	}

	wg.Wait()
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
