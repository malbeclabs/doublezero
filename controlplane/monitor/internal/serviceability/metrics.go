package serviceability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Metrics names.
	MetricNameErrors                                = "doublezero_monitor_serviceability_errors_total"
	MetricNameProgramBuildInfo                      = "doublezero_monitor_serviceability_program_build_info"
	MetricNameUnlinkedInterfaceErrors               = "doublezero_monitor_unlinked_interface_errors_total"
	MetricNameExchangeBGPCommunityDuplicates        = "doublezero_monitor_exchange_bgp_community_duplicates"
	MetricNameExchangeBGPCommunityOutOfRange        = "doublezero_monitor_exchange_bgp_community_out_of_range"
	MetricNameMulticastPublisherBlockTotalIPs       = "doublezero_multicast_publisher_block_total_ips"
	MetricNameMulticastPublisherBlockAllocatedIPs   = "doublezero_multicast_publisher_block_allocated_ips"
	MetricNameMulticastPublisherBlockUtilizationPct = "doublezero_multicast_publisher_block_utilization_percent"

	// Labels.
	MetricLabelErrorType      = "error_type"
	MetricLabelProgramVersion = "program_version"

	// Error types.
	MetricErrorTypeGetProgramData = "get_program_data"
)

var (
	MetricErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameErrors,
			Help: "Number of errors encountered",
		},
		[]string{MetricLabelErrorType},
	)

	MetricProgramBuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricNameProgramBuildInfo,
			Help: "Program build info",
		},
		[]string{MetricLabelProgramVersion},
	)

	MetricUnlinkedInterfaceErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameUnlinkedInterfaceErrors,
			Help: "Onchain error when a device interface is unlinked but participating in an activated link",
		},
		[]string{"device_pubkey", "device_code", "interface_name", "link_pubkey"},
	)

	MetricExchangeBGPCommunityDuplicates = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameExchangeBGPCommunityDuplicates,
			Help: "Onchain error when exchanges have duplicate BGP community values",
		},
		[]string{"exchange_pubkey", "exchange_code", "bgp_community"},
	)

	MetricExchangeBGPCommunityOutOfRange = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameExchangeBGPCommunityOutOfRange,
			Help: "Onchain error when exchange BGP community value is outside valid range (10000-10999)",
		},
		[]string{"exchange_pubkey", "exchange_code", "bgp_community"},
	)

	MetricMulticastPublisherBlockTotalIPs = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: MetricNameMulticastPublisherBlockTotalIPs,
			Help: "Total number of IPs in the multicast publisher block (/21 = 2048 IPs)",
		},
	)

	MetricMulticastPublisherBlockAllocatedIPs = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: MetricNameMulticastPublisherBlockAllocatedIPs,
			Help: "Number of allocated IPs in the multicast publisher block",
		},
	)

	MetricMulticastPublisherBlockUtilizationPct = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: MetricNameMulticastPublisherBlockUtilizationPct,
			Help: "Percentage of multicast publisher block that is allocated (0-100)",
		},
	)
)
