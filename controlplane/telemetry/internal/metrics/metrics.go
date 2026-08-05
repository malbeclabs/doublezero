package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Metrics names.
	MetricNameBuildInfo                        = "doublezero_device_telemetry_agent_build_info"
	MetricNameErrors                           = "doublezero_device_telemetry_agent_errors_total"
	MetricNamePeerDiscoveryLocalTunnelNotFound = "doublezero_device_telemetry_agent_peer_discovery_not_found_tunnels"
	MetricNameEpochCacheStaleAge               = "doublezero_device_telemetry_agent_epoch_cache_stale_age_seconds"
	MetricNameSamplesDropped                   = "doublezero_device_telemetry_agent_samples_dropped_total"
	MetricNamePeers                            = "doublezero_device_telemetry_agent_peers"

	// Labels.
	LabelVersion       = "version"
	LabelCommit        = "commit"
	LabelDate          = "date"
	LabelErrorType     = "error_type"
	LabelLocalDevicePK = "local_device_pk"
	LabelDropReason    = "reason"

	// Error types.
	ErrorTypeCollectorSubmitSamplesOnClose       = "collector_submit_samples_on_close"
	ErrorTypePeerDiscoveryProgramLoad            = "peer_discovery_program_load"
	ErrorTypePeerDiscoveryGettingLocalInterfaces = "peer_discovery_getting_local_interfaces"
	ErrorTypePeerDiscoveryFindingLocalTunnel     = "peer_discovery_finding_local_tunnel"
	ErrorTypePeerDiscoveryLinkTunnelNetInvalid   = "peer_discovery_link_tunnel_net_invalid"
	ErrorTypeSubmitterFailedToInitializeAccount  = "submitter_failed_to_initialize_account"
	ErrorTypeSubmitterFailedToWriteSamples       = "submitter_failed_to_write_samples"
	ErrorTypeSubmitterRetriesExhausted           = "submitter_retries_exhausted"
	// Kept separate rather than folded into one "epoch unavailable" type: a bad ledger URL or boot
	// ordering (never fetched), a multi-hour outage (too stale), and a projected rollover (ended)
	// want different alerts, and the fleet alert fires on the value.
	ErrorTypePingerEpochNeverFetched = "pinger_epoch_never_fetched"
	ErrorTypePingerEpochTooStale     = "pinger_epoch_too_stale"
	ErrorTypePingerEpochEnded        = "pinger_epoch_ended"
	ErrorTypePingerEpochFetchFailed  = "pinger_epoch_fetch_failed"
	// ErrorTypePingerEpochFetch counts once per exhausted retry batch, distinct from
	// ErrorTypePingerEpochFetchFailed's once-per-attempt count.
	ErrorTypePingerEpochFetch     = "pinger_epoch_fetch"
	ErrorTypeSubmitterBufferFull  = "submitter_buffer_full"
	ErrorTypeSubmitterAccountFull = "submitter_account_full"
	// ErrorTypeSubmitterProgramError counts submissions the telemetry program rejected onchain.
	// Distinct from the write/init failure types, which also cover transient RPC trouble: this one
	// only fires on a rejection that will recur until something changes onchain or in config.
	ErrorTypeSubmitterProgramError = "submitter_program_error"

	// Sample drop reasons.
	DropReasonBufferFull  = "buffer_full"
	DropReasonAccountFull = "account_full"
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

	// EpochCacheStaleAge is the age of the cached epoch the probe loop is falling back to while the
	// epoch fetch is failing, 0 whenever the cache is fresh, and +Inf when the fetch is failing and
	// no epoch has ever been cached — the state in which nothing is probed at all.
	EpochCacheStaleAge = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: MetricNameEpochCacheStaleAge,
			Help: "Age of the cached ledger epoch served to the probe loop when the epoch fetch is failing (0 when fresh, +Inf when no epoch has ever been fetched)",
		},
	)
	SamplesDropped = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameSamplesDropped,
			Help: "Number of samples in batches discarded from the buffer without a successful submission, by reason",
		},
		[]string{LabelDropReason},
	)

	Peers = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricNamePeers,
			Help: "Number of peers currently discovered for probing",
		},
		[]string{LabelLocalDevicePK},
	)

	PeerDiscoveryLocalTunnelNotFound = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricNamePeerDiscoveryLocalTunnelNotFound,
			Help: "Number of local tunnel interfaces not found during peer discovery",
		},
		[]string{LabelLocalDevicePK},
	)
)
