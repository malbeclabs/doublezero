package geoprobe

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// Source constants identify which binary is emitting geoprobe metrics.
	SourceGeoProbeAgent = "geoprobe-agent"

	// Metric names.
	MetricNameBuildInfo                    = "doublezero_geoprobe_build_info"
	MetricNameErrors                       = "doublezero_geoprobe_errors_total"
	MetricNameParentDiscoveryDuration      = "doublezero_geoprobe_parent_discovery_duration_seconds"
	MetricNameTargetDiscoveryDuration      = "doublezero_geoprobe_target_discovery_duration_seconds"
	MetricNameMeasurementCycleDuration     = "doublezero_geoprobe_measurement_cycle_duration_seconds"
	MetricNameOffsetsReceived              = "doublezero_geoprobe_offsets_received_total"
	MetricNameOffsetsRejected              = "doublezero_geoprobe_offsets_rejected_total"
	MetricNameCompositeOffsetsSent         = "doublezero_geoprobe_composite_offsets_sent_total"
	MetricNameTargetsDiscovered            = "doublezero_geoprobe_targets_discovered"
	MetricNameParentsDiscovered            = "doublezero_geoprobe_parents_discovered"
	MetricNameIcmpTargetsDiscovered        = "doublezero_geoprobe_icmp_targets_discovered"
	MetricNameIcmpMeasurementCycleDuration = "doublezero_geoprobe_icmp_measurement_cycle_duration_seconds"

	// Labels.
	LabelSource       = "source"
	LabelDevicePubkey = "device_pubkey"
	LabelVersion      = "version"
	LabelCommit       = "commit"
	LabelDate         = "date"
	LabelErrorType    = "error_type"
	LabelReason       = "reason"

	// Error types.
	ErrorTypeMeasurementCycle     = "measurement_cycle"
	ErrorTypeSlotFetch            = "slot_fetch"
	ErrorTypeSignOffset           = "sign_offset"
	ErrorTypeSendOffset           = "send_offset"
	ErrorTypeOffsetReceive        = "offset_receive"
	ErrorTypeIcmpMeasurementCycle = "icmp_measurement_cycle"

	// Offset rejection reasons.
	RejectUnknownParent    = "unknown_parent"
	RejectWrongAuthority   = "wrong_authority"
	RejectInvalidSignature = "invalid_signature"
)

// discoveryBuckets covers RPC-heavy discovery operations which commonly
// take 1-30s depending on network conditions and validator load.
var discoveryBuckets = []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15, 30, 60}

// measurementBuckets covers full measurement cycles which include TWAMP
// probes across multiple targets and can take 30s+.
var measurementBuckets = []float64{0.5, 1, 2.5, 5, 10, 15, 30, 60, 120}

// Metrics holds all Prometheus collectors for the geoprobe subsystem.
type Metrics struct {
	BuildInfo                    *prometheus.GaugeVec
	Errors                       *prometheus.CounterVec
	ParentDiscoveryDuration      prometheus.Histogram
	TargetDiscoveryDuration      prometheus.Histogram
	MeasurementCycleDuration     prometheus.Histogram
	OffsetsReceived              prometheus.Counter
	OffsetsRejected              *prometheus.CounterVec
	CompositeOffsetsSent         prometheus.Counter
	TargetsDiscovered            prometheus.Gauge
	ParentsDiscovered            prometheus.Gauge
	IcmpTargetsDiscovered        prometheus.Gauge
	IcmpMeasurementCycleDuration prometheus.Histogram
}

// NewMetrics creates and registers all geoprobe Prometheus collectors.
// The source and devicePubkey values are applied as constant labels on every metric.
func NewMetrics(source, devicePubkey string, reg prometheus.Registerer) *Metrics {
	constLabels := prometheus.Labels{
		LabelSource:       source,
		LabelDevicePubkey: devicePubkey,
	}

	m := &Metrics{
		BuildInfo: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name:        MetricNameBuildInfo,
				Help:        "Build information of the geoprobe agent",
				ConstLabels: constLabels,
			},
			[]string{LabelVersion, LabelCommit, LabelDate},
		),
		Errors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name:        MetricNameErrors,
				Help:        "Number of errors encountered by the geoprobe agent",
				ConstLabels: constLabels,
			},
			[]string{LabelErrorType},
		),
		ParentDiscoveryDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:        MetricNameParentDiscoveryDuration,
				Help:        "Duration of parent discovery ticks in seconds",
				Buckets:     discoveryBuckets,
				ConstLabels: constLabels,
			},
		),
		TargetDiscoveryDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:        MetricNameTargetDiscoveryDuration,
				Help:        "Duration of target discovery ticks in seconds",
				Buckets:     discoveryBuckets,
				ConstLabels: constLabels,
			},
		),
		MeasurementCycleDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:        MetricNameMeasurementCycleDuration,
				Help:        "Duration of a full measurement cycle in seconds",
				Buckets:     measurementBuckets,
				ConstLabels: constLabels,
			},
		),
		OffsetsReceived: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name:        MetricNameOffsetsReceived,
				Help:        "Total DZD offsets received and cached",
				ConstLabels: constLabels,
			},
		),
		OffsetsRejected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name:        MetricNameOffsetsRejected,
				Help:        "Total DZD offsets rejected",
				ConstLabels: constLabels,
			},
			[]string{LabelReason},
		),
		CompositeOffsetsSent: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name:        MetricNameCompositeOffsetsSent,
				Help:        "Total composite offsets sent to targets",
				ConstLabels: constLabels,
			},
		),
		TargetsDiscovered: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name:        MetricNameTargetsDiscovered,
				Help:        "Current number of discovered targets",
				ConstLabels: constLabels,
			},
		),
		ParentsDiscovered: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name:        MetricNameParentsDiscovered,
				Help:        "Current number of discovered parents",
				ConstLabels: constLabels,
			},
		),
		IcmpTargetsDiscovered: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name:        MetricNameIcmpTargetsDiscovered,
				Help:        "Current number of discovered ICMP targets",
				ConstLabels: constLabels,
			},
		),
		IcmpMeasurementCycleDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:        MetricNameIcmpMeasurementCycleDuration,
				Help:        "Duration of ICMP measurement cycles in seconds",
				Buckets:     measurementBuckets,
				ConstLabels: constLabels,
			},
		),
	}

	reg.MustRegister(
		m.BuildInfo,
		m.Errors,
		m.ParentDiscoveryDuration,
		m.TargetDiscoveryDuration,
		m.MeasurementCycleDuration,
		m.OffsetsReceived,
		m.OffsetsRejected,
		m.CompositeOffsetsSent,
		m.TargetsDiscovered,
		m.ParentsDiscovered,
		m.IcmpTargetsDiscovered,
		m.IcmpMeasurementCycleDuration,
	)

	return m
}
