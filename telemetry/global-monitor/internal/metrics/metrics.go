package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_global_monitor_build_info",
			Help: "Build information of the global monitor",
		},
		[]string{"version", "commit", "date"},
	)

	TickDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "doublezero_global_monitor_tick_duration_seconds",
		Help:    "Duration of the global monitor tick",
		Buckets: prometheus.ExponentialBuckets(10, 1.25, 11), // ~10s .. ~93s
	})

	TickTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_global_monitor_tick_total",
		Help: "Total number of global monitor ticks",
	}, []string{"result"})

	PlanProbesSuccessTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_global_monitor_plan_probes_success_total",
		Help: "Total number of probes that succeeded in the global monitor",
	}, []string{"kind", "path", "probe_type"})

	PlanProbesFailTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_global_monitor_plan_probes_fail_total",
		Help: "Total number of probes that failed in the global monitor",
	}, []string{"kind", "path", "probe_type", "reason"})

	PlanProbesNotReadyTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_global_monitor_plan_probes_not_ready_total",
		Help: "Total number of probes that were not ready in the global monitor",
	}, []string{"kind", "path", "probe_type"})

	ProbesInflight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "doublezero_global_monitor_probes_inflight",
		Help: "Number of probes currently in flight in the global monitor",
	}, []string{"path", "probe_type"})

	ProbeDurations = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "doublezero_global_monitor_probe_durations_seconds",
		Help:    "Duration of probes in the global monitor",
		Buckets: prometheus.ExponentialBuckets(0.005, 1.8, 10), // ≈ 5ms, 9ms, 16ms, 29ms, 52ms, 94ms, 170ms, 305ms, 550ms, 990ms
	}, []string{"path", "probe_type"})

	TargetsCurrent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "doublezero_global_monitor_targets_current",
		Help: "Current number of targets in the global monitor",
	})

	TargetsPrunedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_global_monitor_targets_pruned_total",
		Help: "Total number of targets that were pruned in the global monitor",
	}, []string{"path", "probe_type"})

	TPUQUICDialsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_global_monitor_tpuquic_dials_total",
		Help: "Total number of TPU QUIC dials in the global monitor",
	}, []string{"path", "result"})
)
