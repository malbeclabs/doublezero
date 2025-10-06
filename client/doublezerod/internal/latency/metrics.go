package latency

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	latencyLabels = []string{"device_pk", "device_code", "device_ip"}

	MetricLatencyRtt = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_latency_rtt_nanoseconds",
			Help: "Round-trip time latency measurements to DoubleZero devices in nanoseconds.",
		},
		append(latencyLabels, "stat"), // stat can be "min", "max", "avg"
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
