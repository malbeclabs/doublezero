package server

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	BuildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "doublezero_telemetry_state_ingest_build_info",
		Help: "Build information of the telemetry state ingest server",
	},
		[]string{"version", "commit", "date"},
	)
)
