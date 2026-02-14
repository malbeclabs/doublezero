// Package enricher implements the enricher process and associated annotators.
// The enricher process reads off of redpanda topic containing unenriched flow
// records in protobuf format, enriches the flow with additional information from
// each annotator, and writes the flows as a batch to clickhouse.
//
// Annotators are added via the AddAnnotator method and
// must implement the Annotator interface.
package enricher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/prometheus/client_golang/prometheus"
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

type ServiceabilityFetcher interface {
	GetProgramData(context.Context) (*serviceability.ProgramData, error)
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

func WithServiceabilityFetcher(fetcher ServiceabilityFetcher) EnricherOption {
	return func(e *Enricher) {
		e.serviceability = fetcher
	}
}

func WithServiceabilityFetchInterval(interval time.Duration) EnricherOption {
	return func(e *Enricher) {
		e.serviceabilityFetchInterval = interval
	}
}

type Enricher struct {
	chWriter                    Clicker
	flowConsumer                FlowConsumer
	serviceability              ServiceabilityFetcher
	annotators                  []Annotator
	logger                      *slog.Logger
	metrics                     *EnricherMetrics
	programData                 *serviceability.ProgramData
	programDataMutex            sync.Mutex
	serviceabilityFetchInterval time.Duration
}

func NewEnricher(opts ...EnricherOption) *Enricher {
	e := &Enricher{
		serviceabilityFetchInterval: 10 * time.Second,
		programData:                 &serviceability.ProgramData{},
	}

	for _, opt := range opts {
		opt(e)
	}

	if e.logger == nil {
		e.logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return e
}

// AddAnnotator adds an annotator to the enricher after construction.
func (e *Enricher) AddAnnotator(a Annotator) {
	e.annotators = append(e.annotators, a)
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

	// Make sure we have a serviceability dataset before starting enrichment
	var err error
	e.logger.Info("fetching initial serviceability data")
	e.programDataMutex.Lock()
	e.programData, err = e.serviceability.GetProgramData(ctx)
	e.programDataMutex.Unlock()
	if err != nil {
		return fmt.Errorf("error fetching serviceability data: %v", err)
	}

	go e.fetchServiceabilityData(ctx)

	// initialize annotators before starting enrichment
	if err := e.initAnnotators(ctx); err != nil {
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
				// EOF signals the consumer has no more data (e.g., pcap exhausted)
				if errors.Is(err, io.EOF) {
					e.logger.Info("no more records to consume")
					return nil
				}
				e.logger.Error("error consuming flow records", "error", err)
				continue
			}
			if len(samples) == 0 {
				e.logger.Info("no records to enrich")
				continue
			}

			for i := range samples {
				timer := prometheus.NewTimer(e.metrics.FlowsEnrichmentDuration)
				for _, annotator := range e.annotators {
					if err := annotator.Annotate(&samples[i]); err != nil {
						e.logger.Error("error annotating flow sample", "error", err, "annotator", annotator.String())
						e.metrics.FlowsEnrichmentErrors.Inc()
					}
				}
				timer.ObserveDuration()
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

func (e *Enricher) fetchServiceabilityData(ctx context.Context) {
	ticker := time.NewTicker(e.serviceabilityFetchInterval)
	for {
		select {
		case <-ticker.C:
			newData, err := e.serviceability.GetProgramData(ctx)
			if err != nil {
				e.logger.Error("error refreshing serviceability data", "error", err)
				e.metrics.ServiceabilityFetchErrors.Inc()
				continue
			}
			e.programDataMutex.Lock()
			e.programData = newData
			e.programDataMutex.Unlock()
		case <-ctx.Done():
			ticker.Stop()
			return
		}
	}
}

// ServiceabilityData returns a copy of the current serviceability program data.
// This method is safe for concurrent use.
func (e *Enricher) ServiceabilityData() serviceability.ProgramData {
	e.programDataMutex.Lock()
	defer e.programDataMutex.Unlock()
	if e.programData == nil {
		return serviceability.ProgramData{}
	}
	return *e.programData
}

// Annotator defines the interface for flow enrichment annotators.
// Dependencies should be passed via the annotator's constructor.
type Annotator interface {
	// Init initializes the annotator. Called once before any Annotate calls.
	Init(context.Context) error
	Annotate(*FlowSample) error
	String() string
}

// initAnnotators initializes all registered annotators.
func (e *Enricher) initAnnotators(ctx context.Context) error {
	for _, a := range e.annotators {
		if err := a.Init(ctx); err != nil {
			return fmt.Errorf("error initializing annotator %s: %v", a.String(), err)
		}
	}
	return nil
}
