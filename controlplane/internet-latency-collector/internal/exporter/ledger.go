package exporter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/metrics"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/epoch"
)

const (
	// Default partition buffer capacity provides blocking backpressure on the callers to avoid
	// having too many records in the buffer at once, so that progress is tracked by the callers
	// without risk of losing more than this many samples on ungraceful shutdown. This is important
	// because even though we attempt to flush the buffer to the ledger on shutdown/close, there
	// writing to the ledger takes non-trivial time, and so we don't want to have too many samples
	// in the buffer to flush at those times.
	//
	// This is based on the maximum number of samples that can be written in a single ledger
	// transaction.
	//
	// When the buffer is full reaches this capacity, the calls to `Add` will block until the
	// buffer has space again.
	partitionBufferCapacity = telemetry.MaxSamplesPerBatch
)

type ServiceabilityProgramClient interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

type TelemetryProgramClient interface {
	InitializeInternetLatencySamples(ctx context.Context, config telemetry.InitializeInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error)
	WriteInternetLatencySamples(ctx context.Context, config telemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error)
}

type BufferedLedgerExporterConfig struct {
	Logger         *slog.Logger
	Serviceability ServiceabilityProgramClient
	Telemetry      TelemetryProgramClient

	SubmissionInterval            time.Duration
	OracleAgentPK                 solana.PublicKey
	DataProviderSamplingIntervals map[DataProviderName]time.Duration
	MaxAttempts                   int
	BackoffFunc                   func(attempt int) time.Duration
	EpochFinder                   epoch.Finder
}

func (c *BufferedLedgerExporterConfig) Validate() error {
	if c.EpochFinder == nil {
		return errors.New("epoch finder is required")
	}
	if c.SubmissionInterval <= 0 {
		return errors.New("submission interval must be greater than 0")
	}
	if c.OracleAgentPK.IsZero() {
		return errors.New("oracle agent public key is required")
	}
	if c.DataProviderSamplingIntervals == nil {
		return errors.New("data provider sampling intervals is required")
	}
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Serviceability == nil {
		return errors.New("serviceability program client is required")
	}
	if c.Telemetry == nil {
		return errors.New("telemetry program client is required")
	}
	return nil
}

type BufferedLedgerExporter struct {
	log       *slog.Logger
	cfg       BufferedLedgerExporterConfig
	buffer    *buffer.MemoryPartitionedBuffer[PartitionKey, Sample]
	submitter *Submitter
}

func NewBufferedLedgerExporter(cfg BufferedLedgerExporterConfig) (*BufferedLedgerExporter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate buffered ledger exporter config: %w", err)
	}
	buffer := buffer.NewMemoryPartitionedBuffer[PartitionKey, Sample](partitionBufferCapacity)
	submitter, err := NewSubmitter(cfg.Logger, &SubmitterConfig{
		Buffer:                        buffer,
		Interval:                      cfg.SubmissionInterval,
		OracleAgentPK:                 cfg.OracleAgentPK,
		DataProviderSamplingIntervals: cfg.DataProviderSamplingIntervals,
		Telemetry:                     cfg.Telemetry,
		MaxAttempts:                   cfg.MaxAttempts,
		BackoffFunc:                   cfg.BackoffFunc,
		EpochFinder:                   cfg.EpochFinder,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create submitter: %w", err)
	}

	return &BufferedLedgerExporter{
		log:       cfg.Logger,
		cfg:       cfg,
		buffer:    buffer,
		submitter: submitter,
	}, nil
}

func (e *BufferedLedgerExporter) Buffer() *buffer.MemoryPartitionedBuffer[PartitionKey, Sample] {
	return e.buffer
}

func (e *BufferedLedgerExporter) Run(ctx context.Context) error {
	return e.submitter.Run(ctx)
}

func (e *BufferedLedgerExporter) Close() error {
	return nil
}

func (e *BufferedLedgerExporter) WriteRecords(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}

	// Validate records.
	for _, record := range records {
		if err := record.Validate(); err != nil {
			return fmt.Errorf("invalid record: %w", err)
		}
	}

	// Lookup exchange pubkeys from given codes using serviceability program client.
	exchanges, err := e.getExchanges(ctx)
	if err != nil {
		return fmt.Errorf("failed to get exchanges: %w", err)
	}

	// Add records to partitioned buffer.
	for _, record := range records {
		source, ok := exchanges[record.SourceExchangeCode]
		if !ok {
			e.log.Warn("Source exchange not found, skipping record", "code", record.SourceExchangeCode)
			metrics.ExporterExchangeNotFoundTotal.WithLabelValues(record.SourceExchangeCode).Inc()
			continue
		}
		target, ok := exchanges[record.TargetExchangeCode]
		if !ok {
			e.log.Warn("Target exchange not found, skipping record", "code", record.TargetExchangeCode)
			metrics.ExporterExchangeNotFoundTotal.WithLabelValues(record.TargetExchangeCode).Inc()
			continue
		}

		epoch, err := e.cfg.EpochFinder.ApproximateAtTime(ctx, record.Timestamp)
		if err != nil {
			return fmt.Errorf("failed to get current epoch: %w", err)
		}

		partitionKey := PartitionKey{
			DataProvider:       record.DataProvider,
			SourceExchangePK:   source.PubKey,
			TargetExchangePK:   target.PubKey,
			SourceExchangeCode: record.SourceExchangeCode,
			TargetExchangeCode: record.TargetExchangeCode,
			Epoch:              epoch,
		}
		sample := Sample{
			Timestamp: record.Timestamp,
			RTT:       record.RTT,
		}

		// This will block when the buffer has reached the configured capacity, which is based on
		// the maximum number of samples that can be written in a single ledger transaction.
		//
		// This allows the caller to track progress without risk of losing more than this many
		// samples on ungraceful shutdown.
		size := e.buffer.Add(partitionKey, sample)
		metrics.ExporterPartitionedBufferSize.WithLabelValues(string(partitionKey.DataProvider), partitionKey.SourceExchangePK.String(), partitionKey.TargetExchangePK.String()).Set(float64(size))
	}

	return nil
}

func (e *BufferedLedgerExporter) getExchanges(ctx context.Context) (map[string]serviceability.Exchange, error) {
	serviceabilityData, err := e.cfg.Serviceability.GetProgramData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get program data: %w", err)
	}
	if serviceabilityData == nil {
		return nil, errors.New("serviceability program data is nil")
	}
	exchanges := make(map[string]serviceability.Exchange)
	for _, exchange := range serviceabilityData.Exchanges {
		exchanges[exchange.Code] = exchange
	}
	return exchanges, nil
}
