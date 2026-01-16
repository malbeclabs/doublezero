package gnmi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/ygot/ytypes"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/malbeclabs/doublezero/telemetry/gnmi-writer/internal/gnmi/oc"
)

// Processor orchestrates consuming gNMI notifications and writing records.
// It maintains a registry of extractors that process notifications based on path patterns.
type Processor struct {
	consumer   Consumer
	writer     RecordWriter
	extractors []ExtractorDef
	schema     *ytypes.Schema
	listCache  listSchemaCache // Cached container/list -> schema name mappings
	logger     *slog.Logger
	metrics    *ProcessorMetrics
}

// ProcessorOption configures a Processor.
type ProcessorOption func(*Processor)

// WithConsumer sets the notification consumer.
func WithConsumer(consumer Consumer) ProcessorOption {
	return func(p *Processor) {
		p.consumer = consumer
	}
}

// WithRecordWriter sets the record writer.
func WithRecordWriter(writer RecordWriter) ProcessorOption {
	return func(p *Processor) {
		p.writer = writer
	}
}

// WithProcessorLogger sets the logger.
func WithProcessorLogger(logger *slog.Logger) ProcessorOption {
	return func(p *Processor) {
		p.logger = logger
	}
}

// WithProcessorMetrics sets the processor metrics.
func WithProcessorMetrics(metrics *ProcessorMetrics) ProcessorOption {
	return func(p *Processor) {
		p.metrics = metrics
	}
}

// WithExtractors replaces the default extractors with the provided set.
func WithExtractors(extractors []ExtractorDef) ProcessorOption {
	return func(p *Processor) {
		p.extractors = extractors
	}
}

// WithAdditionalExtractor adds an extractor to the default set.
func WithAdditionalExtractor(name string, match PathMatcher, extract ExtractFunc) ProcessorOption {
	return func(p *Processor) {
		p.extractors = append(p.extractors, ExtractorDef{
			Name:    name,
			Match:   match,
			Extract: extract,
		})
	}
}

// NewProcessor creates a new Processor with the given options.
// By default, it uses DefaultExtractors for processing notifications.
func NewProcessor(opts ...ProcessorOption) (*Processor, error) {
	schema, err := oc.Schema()
	if err != nil {
		return nil, fmt.Errorf("error loading OpenConfig schema: %w", err)
	}

	p := &Processor{
		extractors: DefaultExtractors,
		schema:     schema,
		listCache:  buildListSchemaCache(schema), // Build cache once at startup for O(1) lookups
		metrics:    NewProcessorMetrics(nil),     // Always set, unregistered by default
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.logger == nil {
		p.logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	return p, nil
}

// Run starts the processor and processes notifications until the context is cancelled.
func (p *Processor) Run(ctx context.Context) error {
	if p.consumer == nil {
		return fmt.Errorf("consumer is not initialized")
	}
	if p.writer == nil {
		return fmt.Errorf("writer is not initialized")
	}
	if len(p.extractors) == 0 {
		return fmt.Errorf("no extractors registered")
	}

	defer p.consumer.Close()

	p.logger.Info("starting gNMI processor", "extractors", len(p.extractors))

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("processor shutting down")
			return nil
		default:
			notifications, err := p.consumer.Consume(ctx)
			if err != nil {
				if errors.Is(err, ErrClientClosed) {
					p.logger.Info("consumer client closed, shutting down")
					return nil
				}
				p.logger.Error("error consuming notifications", "error", err)
				continue
			}

			if len(notifications) == 0 {
				continue
			}

			timer := prometheus.NewTimer(p.metrics.ProcessingDuration)
			records := p.processNotifications(ctx, notifications)
			timer.ObserveDuration()

			if len(records) == 0 {
				continue
			}

			if err := p.writer.WriteRecords(ctx, records); err != nil {
				p.logger.Error("error writing records", "error", err)
				p.metrics.WriteErrors.Inc()

				// For non-retryable errors (e.g., table doesn't exist), commit offsets
				// to avoid infinite loop of reprocessing the same messages
				if !IsRetryableClickhouseError(err) {
					p.logger.Warn("non-retryable error, committing offsets to skip messages",
						"error", err,
						"records_dropped", len(records))
					if commitErr := p.consumer.Commit(ctx); commitErr != nil {
						p.logger.Error("error committing offsets", "error", commitErr)
					}
				}
				continue
			}

			if err := p.consumer.Commit(ctx); err != nil {
				p.logger.Error("error committing offsets", "error", err)
				p.metrics.CommitErrors.Inc()
				continue
			}

			p.metrics.RecordsProcessed.Add(float64(len(records)))

			p.logger.Debug("processed notifications", "count", len(records))
		}
	}
}

// processNotifications converts gNMI notifications to Records using registered extractors.
func (p *Processor) processNotifications(ctx context.Context, notifications []*gpb.Notification) []Record {
	var records []Record

	for _, n := range notifications {
		// Check for context cancellation to allow early exit during large batches
		select {
		case <-ctx.Done():
			return records
		default:
		}

		meta := Metadata{
			DevicePubkey: n.GetPrefix().GetTarget(),
			Timestamp:    time.Unix(0, n.GetTimestamp()),
		}

		for _, update := range n.GetUpdate() {
			updatePath := update.GetPath()

			// Find matching extractors
			for _, ext := range p.extractors {
				if !ext.Match(updatePath) {
					continue
				}

				// Unmarshal the notification into an oc.Device
				device, err := p.unmarshalNotification(n, update)
				if err != nil {
					p.logger.Debug("error unmarshaling notification",
						"error", err,
						"extractor", ext.Name,
						"path", pathToString(updatePath))
					p.metrics.ProcessingErrors.Inc()
					continue
				}

				// Extract records
				extractedRecords := ext.Extract(device, meta)
				records = append(records, extractedRecords...)
				break // Only one extractor per update
			}
		}
	}

	// Aggregate records that need deduplication.
	// gNMI sends individual updates for each leaf value, so records with the same
	// key need to be merged into a single row.
	records = AggregateTransceiverState(records)
	records = AggregateTransceiverThresholds(records)

	return records
}

// ProcessNotifications is exported for testing - converts gNMI notifications to Records.
func (p *Processor) ProcessNotifications(ctx context.Context, notifications []*gpb.Notification) []Record {
	return p.processNotifications(ctx, notifications)
}

// Schema returns the ygot schema for testing purposes.
func (p *Processor) Schema() *ytypes.Schema {
	return p.schema
}

// pathToString converts a gNMI path to a string representation.
func pathToString(path *gpb.Path) string {
	if path == nil {
		return ""
	}

	var result string
	for _, elem := range path.GetElem() {
		result += "/" + elem.GetName()
		for k, v := range elem.GetKey() {
			result += "[" + k + "=" + v + "]"
		}
	}
	if result == "" {
		return "/"
	}
	return result
}
