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
	MetricNameMetroBGPCommunityDuplicates           = "doublezero_monitor_metro_bgp_community_duplicates"
	MetricNameMetroBGPCommunityOutOfRange           = "doublezero_monitor_metro_bgp_community_out_of_range"
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

	MetricMetroBGPCommunityDuplicates = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameMetroBGPCommunityDuplicates,
			Help: "Onchain error when metros have duplicate BGP community values",
		},
		[]string{"metro_pubkey", "metro_code", "bgp_community"},
	)

	MetricMetroBGPCommunityOutOfRange = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameMetroBGPCommunityOutOfRange,
			Help: "Onchain error when metro BGP community value is outside valid range (10000-10999)",
		},
		[]string{"metro_pubkey", "metro_code", "bgp_community"},
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
