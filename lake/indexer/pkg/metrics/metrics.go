package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_data_indexer_build_info",
			Help: "Build information of the DoubleZero Data Indexer",
		},
		[]string{"version", "commit", "date"},
	)

	ViewRefreshTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_data_indexer_view_refresh_total",
			Help: "Total number of view refreshes",
		},
		[]string{"view_type", "status"},
	)

	ViewRefreshDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "doublezero_data_indexer_view_refresh_duration_seconds",
			Help:    "Duration of view refreshes",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 12), // 0.1s to ~410s (~6.8 minutes)
		},
		[]string{"view_type"},
	)

	DatabaseQueriesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_data_indexer_database_queries_total",
			Help: "Total number of database queries",
		},
		[]string{"status"},
	)

	DatabaseQueryDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "doublezero_data_indexer_database_query_duration_seconds",
			Help:    "Duration of database queries",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 0.001s to ~4.1s
		},
	)

	MaintenanceOperationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_data_indexer_maintenance_operation_total",
			Help: "Total number of maintenance operations",
		},
		[]string{"operation_type", "status"},
	)

	MaintenanceOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "doublezero_data_indexer_maintenance_operation_duration_seconds",
			Help:    "Duration of maintenance operations",
			Buckets: prometheus.ExponentialBuckets(1, 2, 12), // 1s to ~2048s (~34 minutes)
		},
		[]string{"operation_type"},
	)

	MaintenanceTablesProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_data_indexer_maintenance_tables_processed_total",
			Help: "Total number of tables processed during maintenance operations",
		},
		[]string{"operation_type", "status"},
	)
)
