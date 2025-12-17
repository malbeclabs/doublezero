package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	BuildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "doublezero_telemetry_flow_ingest_build_info",
		Help: "Build information of the telemetry flow ingest",
	}, []string{"version", "commit", "date"})

	UDPPackets = promauto.NewCounter(prometheus.CounterOpts{
		Name: "doublezero_telemetry_flow_ingest_udp_packets_total", Help: "Total UDP packets read from the flow listener.",
	})
	UDPBytes = promauto.NewCounter(prometheus.CounterOpts{
		Name: "doublezero_telemetry_flow_ingest_udp_bytes_total", Help: "Total bytes read from the flow listener.",
	})
	UDPReadErrs = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_telemetry_flow_ingest_udp_read_errors_total", Help: "Total UDP read errors.",
	}, []string{"kind"})
	UDPSetDeadlineErrs = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_telemetry_flow_ingest_udp_set_deadline_errors_total", Help: "Total UDP SetReadDeadline errors.",
	}, []string{"kind"})

	PacketQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "doublezero_telemetry_flow_ingest_udp_queue_depth", Help: "Current depth of the packet channel.",
	})

	FlowDecodeErrs = promauto.NewCounter(prometheus.CounterOpts{
		Name: "doublezero_telemetry_flow_ingest_decode_errors_total", Help: "Total sFlow decode errors.",
	})
	PacketsWithFlowSample = promauto.NewCounter(prometheus.CounterOpts{
		Name: "doublezero_telemetry_flow_ingest_packets_with_flow_sample_total", Help: "Packets that contained at least one FlowSample.",
	})
	PacketsWithoutFlowSample = promauto.NewCounter(prometheus.CounterOpts{
		Name: "doublezero_telemetry_flow_ingest_packets_without_flow_sample_total", Help: "Packets that contained no FlowSample.",
	})

	FlowKafkaProduceOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_telemetry_flow_ingest_flow_kafka_produce_outcomes_total", Help: "Flow produced to Kafka outcomes.",
	}, []string{"result"})
	FlowKafkaProduceInflight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "doublezero_telemetry_flow_ingest_flow_kafka_produce_callbacks_inflight", Help: "Flow produced to Kafka callbacks currently in flight.",
	})

	WorkersRunning = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "doublezero_telemetry_flow_ingest_workers_running", Help: "Number of ingest workers currently running.",
	})

	HealthAccept = promauto.NewCounter(prometheus.CounterOpts{
		Name: "doublezero_telemetry_flow_ingest_health_accept_total", Help: "Total health accepts.",
	})
	HealthAcceptErrs = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_telemetry_flow_ingest_health_accept_errors_total", Help: "Total health accept errors.",
	}, []string{"kind"})
)
