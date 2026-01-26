package gnmi

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MetricsNamespace is the Prometheus namespace for all gnmi-writer metrics.
const MetricsNamespace = "gnmi_writer"

// ConsumerMetrics holds Prometheus metrics for the Kafka consumer.
type ConsumerMetrics struct {
	NotificationsConsumed prometheus.Counter
	FetchErrors           prometheus.Counter
	UnmarshalErrors       prometheus.Counter
}

// NewConsumerMetrics creates consumer metrics registered with the given registerer.
func NewConsumerMetrics(reg prometheus.Registerer) *ConsumerMetrics {
	factory := promauto.With(reg)
	return &ConsumerMetrics{
		NotificationsConsumed: factory.NewCounter(prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "notifications_consumed_total",
			Help:      "Total number of gNMI notifications consumed from Kafka",
		}),
		FetchErrors: factory.NewCounter(prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "fetch_errors_total",
			Help:      "Total number of Kafka fetch errors",
		}),
		UnmarshalErrors: factory.NewCounter(prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "unmarshal_errors_total",
			Help:      "Total number of protobuf unmarshal errors",
		}),
	}
}

// ProcessorMetrics holds Prometheus metrics for the processor.
type ProcessorMetrics struct {
	RecordsProcessed   prometheus.Counter
	ProcessingErrors   prometheus.Counter
	ProcessingDuration prometheus.Histogram
	WriteErrors        prometheus.Counter
	CommitErrors       prometheus.Counter
}

// NewProcessorMetrics creates processor metrics registered with the given registerer.
func NewProcessorMetrics(reg prometheus.Registerer) *ProcessorMetrics {
	factory := promauto.With(reg)
	return &ProcessorMetrics{
		RecordsProcessed: factory.NewCounter(prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "records_processed_total",
			Help:      "Total number of state records processed",
		}),
		ProcessingErrors: factory.NewCounter(prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "processing_errors_total",
			Help:      "Total number of notification processing errors",
		}),
		ProcessingDuration: factory.NewHistogram(prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Name:      "processing_duration_seconds",
			Help:      "Time spent processing notifications",
			Buckets:   prometheus.DefBuckets,
		}),
		WriteErrors: factory.NewCounter(prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "write_errors_total",
			Help:      "Total number of write errors",
		}),
		CommitErrors: factory.NewCounter(prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "commit_errors_total",
			Help:      "Total number of Kafka commit errors",
		}),
	}
}

// ClickhouseMetrics holds Prometheus metrics for the ClickHouse writer.
type ClickhouseMetrics struct {
	InsertDuration prometheus.Histogram
	InsertErrors   prometheus.Counter
	RecordsWritten prometheus.Counter
}

// NewClickhouseMetrics creates ClickHouse metrics registered with the given registerer.
func NewClickhouseMetrics(reg prometheus.Registerer) *ClickhouseMetrics {
	factory := promauto.With(reg)
	return &ClickhouseMetrics{
		InsertDuration: factory.NewHistogram(prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Subsystem: "clickhouse",
			Name:      "insert_duration_seconds",
			Help:      "Time spent inserting batches into ClickHouse",
			Buckets:   prometheus.DefBuckets,
		}),
		InsertErrors: factory.NewCounter(prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: "clickhouse",
			Name:      "insert_errors_total",
			Help:      "Total number of ClickHouse insert errors",
		}),
		RecordsWritten: factory.NewCounter(prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: "clickhouse",
			Name:      "records_written_total",
			Help:      "Total number of records written to ClickHouse",
		}),
	}
}
