package allowlist

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

const metricPrefix = "doublezero_telemetry_allowlist_sync_"

// Metrics are what the section 8 alerts read.
type Metrics struct {
	BuildInfo *prometheus.GaugeVec

	// LastSuccess is the time of the last pass that built the list and left
	// every target holding it (or, in dry run, found what to write). The sync
	// health alert pages when it is 10 minutes old.
	LastSuccess prometheus.Gauge
	// DryRun is 1 while the sync writes nothing. A dry-run pass still sets
	// LastSuccess, so an alert that wants writes reads this too.
	DryRun prometheus.Gauge
	// RefusedPasses counts passes refused by a fail-safe, by reason. The sync
	// health alert pages on any increase.
	RefusedPasses *prometheus.CounterVec
	// Errors counts failed passes and failed report reads, by stage.
	Errors *prometheus.CounterVec
	// Writes counts writes to each target.
	Writes *prometheus.CounterVec
	// ExternalChanges counts, per target, passes that found the target
	// different from how the previous pass of this process left it: a write
	// the sync did not make. It counts passes, not items. The first pass after
	// a start has nothing to compare with and is never counted.
	ExternalChanges *prometheus.CounterVec

	// LiveDevice is 1 for each live device that is not excluded. The missing
	// device alert reads it.
	LiveDevice *prometheus.GaugeVec
	// ListedAddresses and ListedDevices size the list the last pass built.
	ListedAddresses prometheus.Gauge
	ListedDevices   prometheus.Gauge
	// ExceptionReview is 1 for each exception file entry a person should edit.
	ExceptionReview *prometheus.GaugeVec

	// OffchainCaller is the newest call, as a Unix time, per device and
	// calling address the ledger does not hold for that device. listed="false"
	// is an address on no entry for that device, which needs a person.
	OffchainCaller *prometheus.GaugeVec
	// OffchainCallersDropped is how many offchain callers the last report left
	// out over the per-device limit.
	OffchainCallersDropped prometheus.Gauge
	// LastListedCall is, per live device that is not excluded, the newest call
	// from one of its own listed addresses as a Unix time, or 0 when it made
	// none in the caller window.
	LastListedCall *prometheus.GaugeVec
	// ReportLastSuccess is the time the caller addresses were last read.
	// The two report gauges above are as old as this.
	ReportLastSuccess prometheus.Gauge
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		BuildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: metricPrefix + "build_info", Help: "Build information of the allowlist sync.",
		}, []string{"version", "commit", "date"}),
		LastSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: metricPrefix + "last_success_timestamp_seconds", Help: "Unix time of the last successful pass.",
		}),
		DryRun: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: metricPrefix + "dry_run", Help: "1 while the sync writes nothing.",
		}),
		RefusedPasses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricPrefix + "refused_passes_total", Help: "Passes a fail-safe refused, by reason.",
		}, []string{"reason"}),
		Errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricPrefix + "errors_total", Help: "Failed passes and report reads, by stage.",
		}, []string{"stage"}),
		Writes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricPrefix + "writes_total", Help: "Writes to each target.",
		}, []string{"target"}),
		ExternalChanges: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricPrefix + "external_changes_total",
			Help: "Passes that found the target changed by a write the sync did not make, since this process's previous pass left it. Counts passes, not changes; the first pass after a start is never counted.",
		}, []string{"target"}),
		LiveDevice: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: metricPrefix + "live_device", Help: "1 for each live device that is not excluded.",
		}, []string{"pubkey", "code"}),
		ListedAddresses: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: metricPrefix + "listed_addresses", Help: "Addresses on the list the last pass built.",
		}),
		ListedDevices: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: metricPrefix + "listed_devices", Help: "Devices on the list the last pass built.",
		}),
		ExceptionReview: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: metricPrefix + "exception_review", Help: "1 for each exception file entry a person should edit, by reason.",
		}, []string{"kind", "pubkey", "code", "address", "reason"}),
		OffchainCaller: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: metricPrefix + "offchain_caller_last_seen_timestamp_seconds",
			Help: "Newest call per device and calling address the ledger does not hold for that device.",
		}, []string{"pubkey", "code", "caller_ip", "listed"}),
		OffchainCallersDropped: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: metricPrefix + "offchain_callers_dropped",
			Help: "Offchain callers the last report left out over the per-device limit, newest kept.",
		}),
		LastListedCall: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: metricPrefix + "device_last_listed_call_timestamp_seconds",
			Help: "Newest call from a device's own listed addresses, 0 when none in the caller window.",
		}, []string{"pubkey", "code"}),
		ReportLastSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: metricPrefix + "report_last_success_timestamp_seconds", Help: "Unix time the caller addresses were last read.",
		}),
	}
	reg.MustRegister(m.BuildInfo, m.LastSuccess, m.DryRun, m.RefusedPasses, m.Errors, m.Writes, m.ExternalChanges,
		m.LiveDevice, m.ListedAddresses, m.ListedDevices, m.ExceptionReview,
		m.OffchainCaller, m.OffchainCallersDropped, m.LastListedCall, m.ReportLastSuccess)
	// Both targets report 0 from the start, so an alert on an increase has a
	// series to read before the first outside edit.
	for _, target := range []string{"gnmic_map", "cloudflare_list"} {
		m.ExternalChanges.WithLabelValues(target)
	}
	return m
}

func (m *Metrics) setBuilt(b Built) {
	m.LiveDevice.Reset()
	for pk, d := range b.List.Devices {
		m.LiveDevice.WithLabelValues(pk, d.Code).Set(1)
	}
	m.ListedDevices.Set(float64(len(b.List.Devices)))
	m.ListedAddresses.Set(float64(len(b.List.Addrs())))
	m.ExceptionReview.Reset()
	for _, r := range b.Review {
		m.ExceptionReview.WithLabelValues(r.Kind, r.Pubkey, r.Code, r.Address, r.Reason).Set(1)
	}
}

func (m *Metrics) setReport(b Built, r Report) {
	m.OffchainCaller.Reset()
	m.OffchainCallersDropped.Set(float64(r.OffchainDropped))
	for _, c := range r.Offchain {
		m.OffchainCaller.WithLabelValues(c.Pubkey, c.Code, c.CallerIP.String(), strconv.FormatBool(c.Listed)).Set(unixOrZero(c.LastSeen))
	}
	m.LastListedCall.Reset()
	for pk, t := range r.LastListedCall {
		m.LastListedCall.WithLabelValues(pk, b.List.Devices[pk].Code).Set(unixOrZero(t))
	}
}
