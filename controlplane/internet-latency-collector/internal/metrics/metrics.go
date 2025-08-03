package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	ErrorTypeSubmissionRetriesExhausted = "submission_retries_exhausted"
	ErrorTypeGetCurrentEpoch            = "get_current_epoch"
)

var (
	// Build information metric
	BuildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "doublezero_internet_latency_collector_build_info",
		Help: "Build information of the internet latency collector",
	}, []string{"version", "commit", "date"})

	// Blockchain location fetch metrics
	BlockchainLocationFetchTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_internet_latency_collector_blockchain_location_fetch_total",
		Help: "Total number of blockchain location fetch attempts",
	}, []string{"status"})

	BlockchainLocations = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "doublezero_internet_latency_collector_blockchain_locations",
		Help: "Number of locations fetched from blockchain",
	})

	// Common metrics for both collectors
	CollectionRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_internet_latency_collector_collection_runs_total",
		Help: "Total number of successful collection runs",
	}, []string{"data_provider"})

	CollectionFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_internet_latency_collector_collection_failures_total",
		Help: "Total number of failed collection runs",
	}, []string{"data_provider"})

	// RIPE Atlas specific metrics
	RipeatlasMeasurementManagementRunsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "doublezero_internet_latency_collector_ripeatlas_measurement_management_runs_total",
		Help: "Total number of successful ripeatlas measurement management runs",
	})

	RipeatlasMeasurementManagementFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "doublezero_internet_latency_collector_ripeatlas_measurement_management_failures_total",
		Help: "Total number of failed ripeatlas measurement management runs",
	})

	// Wheresitup specific metrics
	WheresitupJobCreationRunsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "doublezero_internet_latency_collector_wheresitup_job_creation_runs_total",
		Help: "Total number of successful wheresitup job creation runs",
	})

	WheresitupJobCreationFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "doublezero_internet_latency_collector_wheresitup_job_creation_failures_total",
		Help: "Total number of failed wheresitup job creation runs",
	})

	WheresitupCreditBalance = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "doublezero_internet_latency_collector_wheresitup_credit_balance",
		Help: "Current Wheresitup credit balance",
	})

	ExporterErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_internet_latency_collector_exporter_errors_total",
		Help: "Total number of errors from the exporter",
	}, []string{"error_type"})

	ExporterLocationNotFoundTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_internet_latency_collector_exporter_location_not_found_total",
		Help: "Total number of location not found warnings from the exporter",
	}, []string{"location"})

	ExporterPartitionedBufferSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "doublezero_internet_latency_collector_exporter_partitioned_buffer_size",
		Help: "Number of partitioned buffers from the exporter",
	}, []string{"data_provider", "source_location_pk", "target_location_pk"})

	ExporterSubmitterAccountFull = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_internet_latency_collector_exporter_submitter_account_full",
		Help: "Number of times the exporter has encountered a submitter account full error",
	}, []string{"data_provider", "source_location_pk", "target_location_pk", "epoch"})
)
