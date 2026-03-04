package exporter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/metrics"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/epoch"
)

const (
	defaultMaxConcurrency = 100
)

type SubmitterConfig struct {
	Interval                      time.Duration
	Buffer                        *buffer.MemoryPartitionedBuffer[PartitionKey, Sample]
	OracleAgentPK                 solana.PublicKey
	DataProviderSamplingIntervals map[DataProviderName]time.Duration
	Telemetry                     TelemetryProgramClient
	BackoffFunc                   func(attempt int) time.Duration // optional, defaults to exponential backoff
	MaxAttempts                   int                             // optional, defaults to 5
	EpochFinder                   epoch.Finder
	MaxConcurrency                int // optional, defaults to 100
}

func (c *SubmitterConfig) Validate() error {
	if c.EpochFinder == nil {
		return errors.New("epoch finder is required")
	}
	if c.Buffer == nil {
		return errors.New("buffer is required")
	}
	if c.Interval <= 0 {
		return errors.New("interval must be greater than 0")
	}
	if c.Telemetry == nil {
		return errors.New("telemetry is required")
	}
	if c.OracleAgentPK.IsZero() {
		return errors.New("oracle agent public key is required")
	}
	if c.MaxConcurrency <= 0 {
		c.MaxConcurrency = defaultMaxConcurrency
	}
	return nil
}

// Submitter periodically flushes collected samples from the sample buffer and submits them to the
// onchain telemetry program.
type Submitter struct {
	log *slog.Logger
	cfg *SubmitterConfig
	rng *rand.Rand
}

func NewSubmitter(log *slog.Logger, cfg *SubmitterConfig) (*Submitter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate submitter config: %w", err)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return &Submitter{
		log: log,
		cfg: cfg,
		rng: rng,
	}, nil
}

func (s *Submitter) Run(ctx context.Context) error {
	s.log.Info("Starting submission loop", "interval", s.cfg.Interval, "maxRetries", s.cfg.MaxAttempts, "oracleAgentPK", s.cfg.OracleAgentPK)

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Debug("Submission loop done, flushing remaining samples")
			// Pass a new context since the current one has already been cancelled.
			s.Tick(context.Background())
			s.log.Debug("Flushed remaining samples")
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
			rtts[j] = uint32(sample.RTT.Microseconds())
			if minTimestamp.IsZero() || sample.Timestamp.Before(minTimestamp) {
				minTimestamp = sample.Timestamp
			}
		}

		writeConfig := telemetry.WriteInternetLatencySamplesInstructionConfig{
			DataProviderName:           string(partitionKey.DataProvider),
			OriginExchangePK:           partitionKey.SourceExchangePK,
			TargetExchangePK:           partitionKey.TargetExchangePK,
			Epoch:                      partitionKey.Epoch,
			StartTimestampMicroseconds: uint64(minTimestamp.UnixMicro()),
			Samples:                    rtts,
		}

		_, _, err := s.cfg.Telemetry.WriteInternetLatencySamples(ctx, writeConfig)
		if err != nil {
			if errors.Is(err, telemetry.ErrAccountNotFound) {
				log.Info("Account not found, initializing new account")
				samplingInterval, ok := s.cfg.DataProviderSamplingIntervals[partitionKey.DataProvider]
				if !ok {
					return fmt.Errorf("no sampling interval found for data provider: %s", partitionKey.DataProvider)
				}
				_, _, err = s.cfg.Telemetry.InitializeInternetLatencySamples(ctx, telemetry.InitializeInternetLatencySamplesInstructionConfig{
					DataProviderName:             string(partitionKey.DataProvider),
					OriginExchangePK:             partitionKey.SourceExchangePK,
					TargetExchangePK:             partitionKey.TargetExchangePK,
					Epoch:                        partitionKey.Epoch,
					SamplingIntervalMicroseconds: uint64(samplingInterval.Microseconds()),
				})
				if err != nil {
					return fmt.Errorf("failed to initialize internet latency samples: %w", err)
				}
				_, _, err = s.cfg.Telemetry.WriteInternetLatencySamples(ctx, writeConfig)
				if err != nil {
					if errors.Is(err, telemetry.ErrSamplesAccountFull) {
						log.Warn("Partition account is full, dropping samples from buffer and moving on", "droppedSamples", len(samples))
						metrics.ExporterSubmitterAccountFull.WithLabelValues(string(partitionKey.DataProvider), partitionKey.SourceExchangePK.String(), partitionKey.TargetExchangePK.String(), strconv.FormatUint(partitionKey.Epoch, 10)).Inc()
						s.cfg.Buffer.Remove(partitionKey)
						return nil
					}
					return fmt.Errorf("failed to write internet latency samples after init: %w", err)
				}
			} else if errors.Is(err, telemetry.ErrSamplesAccountFull) {
				log.Warn("Partition account is full, dropping samples from buffer and moving on", "droppedSamples", len(samples))
				metrics.ExporterSubmitterAccountFull.WithLabelValues(string(partitionKey.DataProvider), partitionKey.SourceExchangePK.String(), partitionKey.TargetExchangePK.String(), strconv.FormatUint(partitionKey.Epoch, 10)).Inc()
				s.cfg.Buffer.Remove(partitionKey)
				return nil
			} else {
				return fmt.Errorf("failed to write internet latency samples: %w", err)
			}
		}

		metrics.ExporterPartitionedBufferSize.WithLabelValues(string(partitionKey.DataProvider), partitionKey.SourceExchangePK.String(), partitionKey.TargetExchangePK.String()).Set(float64(len(samples)))
		log.Debug("Submitted partition samples batch", "count", len(samples), "samples", rtts)
	}

	return nil
}

func (s *Submitter) Tick(ctx context.Context) {
	maxAttempts := s.cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}

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

			// Limit concurrency.
			sem <- struct{}{}
			defer func() { <-sem }()

			tmp := s.cfg.Buffer.CopyAndReset(partitionKey)

			log := s.log.With("partition", partitionKey)

			log.Debug("Submitting samples", "count", len(tmp))

			if len(tmp) == 0 {
				log.Debug("No samples to submit, skipping")
				s.cfg.Buffer.Recycle(partitionKey, tmp)

				// If the account is for a past epoch, remove it.
				currentEpoch, err := s.cfg.EpochFinder.ApproximateAtTime(ctx, time.Now())
				if err != nil {
					log.Error("Failed to get current epoch", "error", err)
					metrics.ExporterErrorsTotal.WithLabelValues(metrics.ErrorTypeGetCurrentEpoch, partitionKey.CircuitCode()).Inc()
					return
				}
				if partitionKey.Epoch < currentEpoch {
					s.cfg.Buffer.Remove(partitionKey)
					log.Debug("Removed partition key")
				}
				return
			}

			success := false
			for attempt := 1; attempt <= maxAttempts; attempt++ {
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
				case maxAttempts:
					metrics.ExporterErrorsTotal.WithLabelValues(metrics.ErrorTypeSubmissionRetriesExhausted, partitionKey.CircuitCode()).Inc()
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
				s.cfg.Buffer.PriorityPrepend(partitionKey, tmp)
			}

			// Always recycle the slice for reuse
			s.cfg.Buffer.Recycle(partitionKey, tmp)

		}(partitionKey)
	}

	wg.Wait()
}

func (s *Submitter) defaultBackoff(attempt int) time.Duration {
	base := 1 * time.Second
	max := 10 * time.Second
	jitter := 0.5 + 0.5*s.rng.Float64()
	mult := 1 << uint(attempt-1)
	backoff := time.Duration(float64(base) * float64(mult) * jitter)
	if backoff > max {
		return max
	}
	return backoff
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
