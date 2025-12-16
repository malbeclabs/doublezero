package enricher

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type EnricherMetrics struct {
	FlowsProcessedTotal       prometheus.Counter
	FlowsEnrichedTotal        prometheus.Counter
	FlowsEnrichmentFailed     prometheus.Counter
	FlowsEnrichmentDuration   prometheus.Histogram
	ClickhouseInsertErrors    prometheus.Counter
	KafkaConsumeErrors        prometheus.Counter
	KafkaCommitErrors         prometheus.Counter
	ServiceabilityFetchErrors prometheus.Counter
}

func NewEnricherMetrics(reg prometheus.Registerer) *EnricherMetrics {
	factory := promauto.With(reg)

	return &EnricherMetrics{
		FlowsProcessedTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "flows_processed_total",
			Help: "Total number of flows processed",
		}),
		FlowsEnrichedTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "flows_enriched_total",
			Help: "Total number of flows successfully enriched",
		}),
		FlowsEnrichmentFailed: factory.NewCounter(prometheus.CounterOpts{
			Name: "flows_enrichment_failed_total",
			Help: "Total number of flows that failed enrichment",
		}),
		ClickhouseInsertErrors: factory.NewCounter(prometheus.CounterOpts{
			Name: "clickhouse_insert_errors_total",
			Help: "Total number of errors inserting into Clickhouse",
		}),
		KafkaConsumeErrors: factory.NewCounter(prometheus.CounterOpts{
			Name: "kafka_consume_errors_total",
			Help: "Total number of errors consuming from Kafka",
		}),
		FlowsEnrichmentDuration: factory.NewHistogram(prometheus.HistogramOpts{
			Name: "flows_enrichment_duration_seconds",
			Help: "Duration of flow enrichment in seconds",
		}),
		KafkaCommitErrors: factory.NewCounter(prometheus.CounterOpts{
			Name: "kafka_commit_errors_total",
			Help: "Total number of errors committing offsets to Kafka",
		}),
		ServiceabilityFetchErrors: factory.NewCounter(prometheus.CounterOpts{
			Name: "serviceability_fetch_errors_total",
			Help: "Total number of errors fetching serviceability data",
		}),
	}
}

type ClickhouseMetrics struct {
	InsertDuration prometheus.Histogram
}

func NewClickhouseMetrics(reg prometheus.Registerer) *ClickhouseMetrics {
	factory := promauto.With(reg)

	return &ClickhouseMetrics{
		InsertDuration: factory.NewHistogram(prometheus.HistogramOpts{
			Name: "clickhouse_insert_duration_seconds",
			Help: "Duration of Clickhouse insert operations in seconds",
		}),
	}
}

type FlowConsumerMetrics struct {
	FlowsDecodedTotal   prometheus.Counter
	FlowDecodeErrors    prometheus.Counter
	FlowUnmarshalErrors prometheus.Counter
}

func NewFlowConsumerMetrics(reg prometheus.Registerer) *FlowConsumerMetrics {
	factory := promauto.With(reg)

	return &FlowConsumerMetrics{
		FlowDecodeErrors: factory.NewCounter(prometheus.CounterOpts{
			Name: "flow_decode_errors_total",
			Help: "Total number of flow decode errors",
		}),
		FlowUnmarshalErrors: factory.NewCounter(prometheus.CounterOpts{
			Name: "flow_unmarshal_errors_total",
			Help: "Total number of flow unmarshal errors",
		}),
		FlowsDecodedTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "flows_decoded_total",
			Help: "Total number of flows decoded",
		}),
	}
}
