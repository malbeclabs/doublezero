package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	Namespace = "doublezero_device_telemetry_agent"

	// Metrics names.
	MetricNameBuildInfo                        = Namespace + "_build_info"
	MetricNameErrors                           = Namespace + "_errors_total"
	MetricNamePeerDiscoveryLocalTunnelNotFound = Namespace + "_peer_discovery_local_tunnel_not_found_total"

	// Labels.
	LabelVersion   = "version"
	LabelCommit    = "commit"
	LabelDate      = "date"
	LabelErrorType = "error_type"

	// Error types.
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
			Name: MetricNameBuildInfo,
			Help: "Build information of the device telemetry agent",
		},
		[]string{LabelVersion, LabelCommit, LabelDate},
	)

	Errors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameErrors,
			Help: "Number of errors encountered",
		},
		[]string{LabelErrorType},
	)

	PeerDiscoveryLocalTunnelNotFound = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: MetricNamePeerDiscoveryLocalTunnelNotFound,
			Help: "Number of local tunnel interfaces not found encountered during peer discovery",
		},
	)
)
