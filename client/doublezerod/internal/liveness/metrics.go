package liveness

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Labels.
	LabelIface     = "iface"
	LabelLocalIP   = "local_ip"
	LabelPeerIP    = "peer_ip"
	LabelState     = "state"
	LabelStateTo   = "state_to"
	LabelStateFrom = "state_from"
	LabelReason    = "reason"
	LabelOperation = "operation"
)

var (
	serviceLabels = []string{LabelIface, LabelLocalIP}
)

func withServiceLabels(labels ...string) []string {
	return append(serviceLabels, labels...)
}

var (
	metricSessions = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_liveness_sessions",
			Help: "Current number of sessions by FSM state",
		},
		withServiceLabels(LabelState),
	)

	metricSessionTransitions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_liveness_session_transitions_total",
			Help: "Count of session state transitions",
		},
		withServiceLabels(LabelStateFrom, LabelStateTo, LabelReason),
	)

	metricRoutesInstalled = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_liveness_routes_installed",
			Help: "Number of routes installed",
		},
		serviceLabels,
	)

	metricRouteInstalls = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_liveness_route_installs_total",
			Help: "Count of route installs",
		},
		serviceLabels,
	)

	metricRouteWithdraws = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_liveness_route_withdraws_total",
			Help: "Count of route withdraws",
		},
		serviceLabels,
	)

	metricConvergenceToUp = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "doublezero_liveness_convergence_to_up_seconds",
			Help: "Time from first successful control message while down until transition to up (includes detect threshold, scheduler delay, and kernel install).",
		},
		serviceLabels,
	)

	metricConvergenceToDown = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "doublezero_liveness_convergence_to_down_seconds",
			Help: "Time from first failed or missing control message while up until transition to down (includes detect expiry, scheduler delay, and kernel delete).",
		},
		serviceLabels,
	)

	metricSchedulerServiceQueueLen = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_liveness_scheduler_service_queue_len",
			Help: "Current number of pending events in the scheduler queue per service (iface, local_ip)",
		},
		serviceLabels,
	)

	metricSchedulerEventsDropped = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_liveness_scheduler_events_dropped_total",
			Help: "Scheduler events dropped by type and reason.",
		},
		[]string{"type", "reason"},
	)

	metricSchedulerTotalQueueLen = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "doublezero_liveness_scheduler_total_queue_len",
			Help: "Total events currently in the scheduler queue.",
		},
	)

	metricRouteInstallFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_liveness_route_install_failures_total",
			Help: "Count of route kernel install failures",
		},
		serviceLabels,
	)

	metricRouteUninstallFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_liveness_route_uninstall_failures_total",
			Help: "Count of route kernel uninstall failures",
		},
		serviceLabels,
	)

	metricHandleRxDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "doublezero_liveness_handle_rx_duration_seconds",
			Help: "Distribution of time to handle a valid received packet.",
		},
		serviceLabels,
	)

	metricControlPacketsTX = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_liveness_control_packets_tx_total",
			Help: "Total control packets sent.",
		},
		serviceLabels, // iface, local_ip
	)

	metricControlPacketsRX = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_liveness_control_packets_rx_total",
			Help: "Total control packets received.",
		},
		serviceLabels,
	)

	metricControlPacketsRxInvalid = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_liveness_control_packets_rx_invalid_total",
			Help: "Invalid control packets received (e.g. short, bad_version, bad_len, parse_error, not_ipv4, reserved_nonzero).",
		},
		withServiceLabels(LabelReason),
	)

	metricUnknownPeerPackets = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_liveness_unknown_peer_packets_total",
			Help: "Packets received that didn’t match any known session.",
		},
		serviceLabels,
	)

	metricReadSocketErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_liveness_read_socket_errors_total",
			Help: "Count of read socket errors.",
		},
		serviceLabels,
	)

	metricWriteSocketErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_liveness_write_socket_errors_total",
			Help: "Count of write socket errors.",
		},
		serviceLabels,
	)

	// Per peer metrics for route liveness (high cardinality).
	metricRouteLivenessSessions = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_liveness_peer_sessions",
			Help: "Current number of sessions by peer and FSM state",
		},
		withServiceLabels(LabelPeerIP, LabelState),
	)

	metricPeerDetectTime = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_liveness_peer_session_detect_time_seconds",
			Help: "Current detect time by session (after clamping with peer value).",
		},
		withServiceLabels(LabelPeerIP),
	)
)

func emitSessionStateMetrics(sess *Session, prevState *State, operation string, peerMetrics bool) {
	if sess == nil || sess.peer == nil {
		return
	}
	var prevStateStr string
	if prevState != nil {
		prevStateStr = prevState.String()
	} else {
		prevStateStr = "NEW"
	}
	metricSessionTransitions.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP, prevStateStr, sess.state.String(), operation).Inc()
	metricSessions.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP, sess.state.String()).Inc()
	if prevState != nil {
		metricSessions.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP, prevStateStr).Dec()
	}
	if peerMetrics {
		metricRouteLivenessSessions.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP, sess.peer.PeerIP, sess.state.String()).Inc()
		if prevState != nil {
			metricRouteLivenessSessions.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP, sess.peer.PeerIP, prevStateStr).Dec()
		}
	}
}

func emitRouteInstallMetrics(iface, localIP string) {
	metricRoutesInstalled.WithLabelValues(iface, localIP).Inc()
	metricRouteInstalls.WithLabelValues(iface, localIP).Inc()
}

func emitRouteWithdrawMetrics(iface, localIP string) {
	metricRoutesInstalled.WithLabelValues(iface, localIP).Dec()
	metricRouteWithdraws.WithLabelValues(iface, localIP).Inc()
}

func emitPeerDetectTimeGauge(sess *Session, dt time.Duration) {
	if sess == nil || sess.peer == nil {
		return
	}
	metricPeerDetectTime.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP, sess.peer.PeerIP).Set(dt.Seconds())
}

func metricsCleanupOnWithdrawRoute(sess *Session, peerMetrics bool) {
	if sess == nil || sess.peer == nil {
		return
	}
	metricSessions.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP, sess.state.String()).Dec()
	if peerMetrics {
		metricRouteLivenessSessions.DeleteLabelValues(sess.peer.Interface, sess.peer.LocalIP, sess.peer.PeerIP, StateDown.String())
		metricRouteLivenessSessions.DeleteLabelValues(sess.peer.Interface, sess.peer.LocalIP, sess.peer.PeerIP, StateInit.String())
		metricRouteLivenessSessions.DeleteLabelValues(sess.peer.Interface, sess.peer.LocalIP, sess.peer.PeerIP, StateUp.String())
		metricRouteLivenessSessions.DeleteLabelValues(sess.peer.Interface, sess.peer.LocalIP, sess.peer.PeerIP, StateAdminDown.String())
		metricPeerDetectTime.DeleteLabelValues(sess.peer.Interface, sess.peer.LocalIP, sess.peer.PeerIP)
	}
}

func emitConvergenceToUpMetrics(sess *Session, convergence time.Duration) {
	if sess == nil || sess.peer == nil {
		return
	}
	metricConvergenceToUp.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP).Observe(convergence.Seconds())
}

func emitConvergenceToDownMetrics(sess *Session, convergence time.Duration) {
	if sess == nil || sess.peer == nil {
		return
	}
	metricConvergenceToDown.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP).Observe(convergence.Seconds())
}

func emitSchedulerServiceQueueLengthGauge(eq *EventQueue, sess *Session) {
	if sess == nil || sess.peer == nil {
		return
	}
	iface, lip := sess.peer.Interface, sess.peer.LocalIP
	cnt := eq.CountFor(iface, lip)
	if cnt == 0 {
		metricSchedulerServiceQueueLen.DeleteLabelValues(iface, lip)
	} else {
		metricSchedulerServiceQueueLen.WithLabelValues(iface, lip).Set(float64(cnt))
	}
}
