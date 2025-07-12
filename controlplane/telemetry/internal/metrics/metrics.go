package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	ErrorTypeCollectorSubmitSamplesOnClose       = "collector_submit_samples_on_close"
	ErrorTypePeerDiscoveryProgramLoad            = "peer_discovery_program_load"
	ErrorTypePeerDiscoveryGettingLocalInterfaces = "peer_discovery_getting_local_interfaces"
	ErrorTypePeerDiscoveryFindingLocalTunnel     = "peer_discovery_finding_local_tunnel"
	ErrorTypePeerDiscoveryLinkTunnelNetInvalid   = "peer_discovery_link_tunnel_net_invalid"
	ErrorTypeSubmitterFailedToInitializeAccount  = "submitter_failed_to_initialize_account"
	ErrorTypeSubmitterFailedToWriteSamples       = "submitter_failed_to_write_samples"
	ErrorTypeSubmitterRetriesExhausted           = "submitter_retries_exhausted"
)

var (
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_device_telemetry_agent_build_info",
			Help: "Build information of the device telemetry agent",
		},
		[]string{"version", "commit", "date"},
	)

	Errors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_device_telemetry_agent_errors_total",
			Help: "Number of errors encountered",
		},
		[]string{"error_type"},
	)

	PeerDiscoveryLocalTunnelNotFound = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "doublezero_device_telemetry_agent_peer_discovery_local_tunnel_not_found_total",
			Help: "Number of local tunnel interfaces not found encountered during peer discovery",
		},
	)
)
