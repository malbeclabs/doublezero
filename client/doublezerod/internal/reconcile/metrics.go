package reconcile

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	reinstalls *prometheus.CounterVec
	failures   *prometheus.CounterVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		reinstalls: registerCounterVec(reg, prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_route_reconcile_reinstalls_total",
				Help: "Count of BGP routes reinstalled after being removed from the kernel by an external process",
			},
			[]string{"local_ip"},
		)),
		failures: registerCounterVec(reg, prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_route_reconcile_failures_total",
				Help: "Count of failed attempts to reinstall a missing BGP route during reconciliation",
			},
			[]string{"local_ip"},
		)),
	}
}

// registerCounterVec registers c, or returns the collector already registered
// under its name, so constructing a second Reconciler against the same
// registry (a runtime restart in one process) does not panic.
func registerCounterVec(reg prometheus.Registerer, c *prometheus.CounterVec) *prometheus.CounterVec {
	if err := reg.Register(c); err != nil {
		var are prometheus.AlreadyRegisteredError
		if errors.As(err, &are) {
			if existing, ok := are.ExistingCollector.(*prometheus.CounterVec); ok {
				return existing
			}
		}
		panic(err)
	}
	return c
}
