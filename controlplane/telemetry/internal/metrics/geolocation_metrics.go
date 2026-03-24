package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// GeoProbe agent metric names.
	GeoProbeMetricNameBuildInfo                = "doublezero_device_geoprobe_agent_build_info"
	GeoProbeMetricNameErrors                   = "doublezero_device_geoprobe_agent_errors_total"
	GeoProbeMetricNameParentDiscoveryDuration  = "doublezero_device_geoprobe_agent_parent_discovery_duration_seconds"
	GeoProbeMetricNameTargetDiscoveryDuration  = "doublezero_device_geoprobe_agent_target_discovery_duration_seconds"
	GeoProbeMetricNameMeasurementCycleDuration = "doublezero_device_geoprobe_agent_measurement_cycle_duration_seconds"
	GeoProbeMetricNameOffsetsReceived          = "doublezero_device_geoprobe_agent_offsets_received_total"
	GeoProbeMetricNameOffsetsRejected          = "doublezero_device_geoprobe_agent_offsets_rejected_total"
	GeoProbeMetricNameCompositeOffsetsSent     = "doublezero_device_geoprobe_agent_composite_offsets_sent_total"
	GeoProbeMetricNameTargetsDiscovered        = "doublezero_device_geoprobe_agent_targets_discovered"
	GeoProbeMetricNameParentsDiscovered        = "doublezero_device_geoprobe_agent_parents_discovered"

	// GeoProbe agent labels.
	GeoProbeMetricLabelReason = "reason"

	// GeoProbe agent error types.
	GeoProbeErrorTypeParentDiscovery  = "parent_discovery"
	GeoProbeErrorTypeTargetDiscovery  = "target_discovery"
	GeoProbeErrorTypeMeasurementCycle = "measurement_cycle"
	GeoProbeErrorTypeSlotFetch        = "slot_fetch"
	GeoProbeErrorTypeSignOffset       = "sign_offset"
	GeoProbeErrorTypeSendOffset       = "send_offset"
	GeoProbeErrorTypeOffsetReceive    = "offset_receive"

	// Offset rejection reasons.
	GeoProbeRejectUnknownParent    = "unknown_parent"
	GeoProbeRejectWrongAuthority   = "wrong_authority"
	GeoProbeRejectInvalidSignature = "invalid_signature"
)

var (
	GeoProbeBuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: GeoProbeMetricNameBuildInfo,
			Help: "Build information of the geoprobe agent",
		},
		[]string{LabelVersion, LabelCommit, LabelDate},
	)

	GeoProbeErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: GeoProbeMetricNameErrors,
			Help: "Number of errors encountered by the geoprobe agent",
		},
		[]string{LabelErrorType},
	)

	GeoProbeParentDiscoveryDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name: GeoProbeMetricNameParentDiscoveryDuration,
			Help: "Duration of parent discovery ticks in seconds",
		},
	)

	GeoProbeTargetDiscoveryDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name: GeoProbeMetricNameTargetDiscoveryDuration,
			Help: "Duration of target discovery ticks in seconds",
		},
	)

	GeoProbeMeasurementCycleDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name: GeoProbeMetricNameMeasurementCycleDuration,
			Help: "Duration of a full measurement cycle in seconds",
		},
	)

	GeoProbeOffsetsReceived = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: GeoProbeMetricNameOffsetsReceived,
			Help: "Total DZD offsets received and cached",
		},
	)

	GeoProbeOffsetsRejected = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: GeoProbeMetricNameOffsetsRejected,
			Help: "Total DZD offsets rejected",
		},
		[]string{GeoProbeMetricLabelReason},
	)

	GeoProbeCompositeOffsetsSent = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: GeoProbeMetricNameCompositeOffsetsSent,
			Help: "Total composite offsets sent to targets",
		},
	)

	GeoProbeTargetsDiscovered = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: GeoProbeMetricNameTargetsDiscovered,
			Help: "Current number of discovered targets",
		},
	)

	GeoProbeParentsDiscovered = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: GeoProbeMetricNameParentsDiscovered,
			Help: "Current number of discovered parents",
		},
	)
)
