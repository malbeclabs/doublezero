// Package enricher implements the enricher process and associated annotators.
// The enricher process reads off of redpanda topic containing unenriched flow
// records in protobuf format, enriches the flow with additional information from
// each annotator, and writes the flows as a batch to clickhouse.
//
// Annotators must be registered in the RegisterAnnotators method of the enricher
// and must implement the Annotator interface.
package enricher

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// FlowConsumer defines the minimal interface for consuming flow records.
type FlowConsumer interface {
	ConsumeFlowRecords(ctx context.Context) ([]FlowSample, error)
	CommitOffsets(ctx context.Context) error
	Close() error
}

// Clicker defines the minimal interface the Enricher needs to interact with ClickHouse.
type Clicker interface {
	BatchInsert(context.Context, []FlowSample) error
}

type EnricherOption func(*Enricher)

// WithClickhouseWriter injects a Clicker implementation into the Enricher.
func WithClickhouseWriter(writer Clicker) EnricherOption {
	return func(e *Enricher) {
		e.chWriter = writer
	}
}

// WithFlowConsumer injects a FlowConsumer implementation into the Enricher.
func WithFlowConsumer(consumer FlowConsumer) EnricherOption {
	return func(e *Enricher) {
		e.flowConsumer = consumer
	}
}

func WithLogger(logger *slog.Logger) EnricherOption {
	return func(e *Enricher) {
		e.logger = logger
	}
}

// WithEnricherMetrics injects prometheus metrics into the Enricher.
func WithEnricherMetrics(metrics *EnricherMetrics) EnricherOption {
	return func(e *Enricher) {
		e.metrics = metrics
	}
}

type Enricher struct {
	chWriter     Clicker
	flowConsumer FlowConsumer
	annotators   []Annotator
	logger       *slog.Logger
	metrics      *EnricherMetrics
}

func NewEnricher(opts ...EnricherOption) *Enricher {
	e := &Enricher{}

	for _, opt := range opts {
		opt(e)
	}
	if e.logger == nil {
		e.logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return e
}

// Run starts the enricher instance and begins processing flow records.
func (e *Enricher) Run(ctx context.Context) error {
	if e.flowConsumer == nil {
		return fmt.Errorf("flow consumer is not initialized")
	}
	if e.chWriter == nil {
		return fmt.Errorf("clickhouse connection is not initialized")
	}
	defer e.flowConsumer.Close()

	if err := e.RegisterAnnotators(ctx); err != nil {
		return fmt.Errorf("error while initializing annotators: %v", err)
	}

	// Let's annotate some flow records
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			samples, err := e.flowConsumer.ConsumeFlowRecords(ctx)
			if err != nil {
				e.logger.Error("error consuming flow records", "error", err)
				continue
			}
			if len(samples) == 0 {
				continue
			}

			if err := e.chWriter.BatchInsert(ctx, samples); err != nil {
				e.logger.Error("error inserting batch via Clicker", "error", err)
				e.metrics.ClickhouseInsertErrors.Inc()
				continue
			}

			if err := e.flowConsumer.CommitOffsets(ctx); err != nil {
				e.logger.Error("commit records failed", "error", err)
				e.metrics.KafkaCommitErrors.Inc()
				continue
			}
			e.metrics.FlowsProcessedTotal.Add(float64(len(samples)))
		}
	}
}

type Annotator interface {
	Init(context.Context, clickhouse.Conn) error
	Annotate(*FlowSample) error
	String() string
}

// RegisterAnnotators initializes a set of annotators for use during enrichment.
// Annotators must implement the Annotator interface.
func (e *Enricher) RegisterAnnotators(ctx context.Context) error {
	e.annotators = []Annotator{
		// NewIfNameAnnotator(),
	}

	for _, a := range e.annotators {
		// TODO: The clickhouse connection is passed here but it's nil.
		if err := a.Init(ctx, nil); err != nil {
			return fmt.Errorf("error initializing annotator %s: %v", a.String(), err)
		}
	}
	return nil
}
