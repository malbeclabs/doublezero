package latency

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	latencyLabels = []string{"device_pk", "device_code", "device_ip"}

	MetricLatencyRttMin = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_latency_rtt_min_nanoseconds",
			Help: "Minimum round-trip time latency to DoubleZero devices in nanoseconds.",
		},
		latencyLabels,
	)
	MetricLatencyRttAvg = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_latency_rtt_avg_nanoseconds",
			Help: "Average round-trip time latency to DoubleZero devices in nanoseconds.",
		},
		latencyLabels,
	)
	MetricLatencyRttMax = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_latency_rtt_max_nanoseconds",
			Help: "Maximum round-trip time latency to DoubleZero devices in nanoseconds.",
		},
		latencyLabels,
	)

	MetricLatencyLoss = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_latency_loss_percentage",
			Help: "Packet loss percentage to DoubleZero devices.",
		},
		latencyLabels,
	)

	MetricLatencyReachable = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_latency_reachable",
			Help: "Indicates if a device is reachable (1 for reachable, 0 for unreachable).",
		},
		latencyLabels,
	)
)
