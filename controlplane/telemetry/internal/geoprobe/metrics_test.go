package geoprobe

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestNewMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(SourceGeoProbeAgent, "DevPK123", reg)

	if m.BuildInfo == nil {
		t.Fatal("BuildInfo is nil")
	}
	if m.Errors == nil {
		t.Fatal("Errors is nil")
	}
	if m.ParentDiscoveryDuration == nil {
		t.Fatal("ParentDiscoveryDuration is nil")
	}
	if m.TargetDiscoveryDuration == nil {
		t.Fatal("TargetDiscoveryDuration is nil")
	}
	if m.MeasurementCycleDuration == nil {
		t.Fatal("MeasurementCycleDuration is nil")
	}
	if m.OffsetsReceived == nil {
		t.Fatal("OffsetsReceived is nil")
	}
	if m.OffsetsRejected == nil {
		t.Fatal("OffsetsRejected is nil")
	}
	if m.CompositeOffsetsSent == nil {
		t.Fatal("CompositeOffsetsSent is nil")
	}
	if m.TargetsDiscovered == nil {
		t.Fatal("TargetsDiscovered is nil")
	}
	if m.ParentsDiscovered == nil {
		t.Fatal("ParentsDiscovered is nil")
	}
	if m.IcmpTargetsDiscovered == nil {
		t.Fatal("IcmpTargetsDiscovered is nil")
	}
	if m.IcmpMeasurementCycleDuration == nil {
		t.Fatal("IcmpMeasurementCycleDuration is nil")
	}
}

func TestNewMetrics_ConstantLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(SourceGeoProbeAgent, "DevPK456", reg)

	// Increment a counter to make it collectible.
	m.Errors.WithLabelValues(ErrorTypeMeasurementCycle).Inc()

	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatal("Failed to gather metrics:", err)
	}

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == MetricNameErrors {
			found = true
			metric := mf.GetMetric()[0]
			assertLabel(t, metric, LabelSource, SourceGeoProbeAgent)
			assertLabel(t, metric, LabelDevicePubkey, "DevPK456")
			assertLabel(t, metric, LabelErrorType, ErrorTypeMeasurementCycle)
		}
	}
	if !found {
		t.Fatal("errors_total metric not found in gathered metrics")
	}
}

func TestNewMetrics_RegistersTwelveCollectors(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(SourceGeoProbeAgent, "DevPK789", reg)

	// Touch vec-based metrics so they appear in Gather output.
	m.BuildInfo.WithLabelValues("v1", "abc", "today").Set(1)
	m.Errors.WithLabelValues(ErrorTypeMeasurementCycle).Inc()
	m.OffsetsRejected.WithLabelValues(RejectUnknownParent).Inc()

	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatal("Failed to gather metrics:", err)
	}

	expectedNames := map[string]bool{
		MetricNameBuildInfo:                    false,
		MetricNameErrors:                       false,
		MetricNameParentDiscoveryDuration:      false,
		MetricNameTargetDiscoveryDuration:      false,
		MetricNameMeasurementCycleDuration:     false,
		MetricNameOffsetsReceived:              false,
		MetricNameOffsetsRejected:              false,
		MetricNameCompositeOffsetsSent:         false,
		MetricNameTargetsDiscovered:            false,
		MetricNameParentsDiscovered:            false,
		MetricNameIcmpTargetsDiscovered:        false,
		MetricNameIcmpMeasurementCycleDuration: false,
	}

	for _, mf := range metricFamilies {
		if _, ok := expectedNames[mf.GetName()]; ok {
			expectedNames[mf.GetName()] = true
		}
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("expected metric %q not found in gathered output", name)
		}
	}
}

func TestNewMetrics_DuplicateRegistrationPanics(t *testing.T) {
	reg := prometheus.NewRegistry()
	NewMetrics(SourceGeoProbeAgent, "PK1", reg)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	NewMetrics(SourceGeoProbeAgent, "PK1", reg)
}

func assertLabel(t *testing.T, metric *dto.Metric, name, expectedValue string) {
	t.Helper()
	for _, lp := range metric.GetLabel() {
		if lp.GetName() == name {
			if lp.GetValue() != expectedValue {
				t.Errorf("label %q: got %q, want %q", name, lp.GetValue(), expectedValue)
			}
			return
		}
	}
	t.Errorf("label %q not found on metric", name)
}
