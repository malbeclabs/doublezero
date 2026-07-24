package reconcile

import "github.com/prometheus/client_golang/prometheus"

type metrics struct {
	reinstalls *prometheus.CounterVec
	failures   *prometheus.CounterVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{
		reinstalls: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_route_reconcile_reinstalls_total",
				Help: "Count of BGP routes reinstalled after being removed from the kernel by an external process",
			},
			[]string{"local_ip"},
		),
		failures: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_route_reconcile_failures_total",
				Help: "Count of failed attempts to reinstall a missing BGP route during reconciliation",
			},
			[]string{"local_ip"},
		),
	}
	reg.MustRegister(m.reinstalls, m.failures)
	return m
}
