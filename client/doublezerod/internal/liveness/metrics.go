package liveness

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
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

type Metrics struct {
	Sessions                 *prometheus.GaugeVec
	SessionTransitions       *prometheus.CounterVec
	RoutesInstalled          *prometheus.GaugeVec
	RouteInstalls            *prometheus.CounterVec
	RouteWithdraws           *prometheus.CounterVec
	ConvergenceToUp          *prometheus.HistogramVec
	ConvergenceToDown        *prometheus.HistogramVec
	SchedulerServiceQueueLen *prometheus.GaugeVec
	SchedulerEventsDropped   *prometheus.CounterVec
	SchedulerTotalQueueLen   prometheus.Gauge
	RouteInstallFailures     *prometheus.CounterVec
	RouteUninstallFailures   *prometheus.CounterVec
	HandleRxDuration         *prometheus.HistogramVec
	ControlPacketsTX         *prometheus.CounterVec
	ControlPacketsRX         *prometheus.CounterVec
	ControlPacketsRxInvalid  *prometheus.CounterVec
	UnknownPeerPackets       *prometheus.CounterVec
	ReadSocketErrors         *prometheus.CounterVec
	WriteSocketErrors        *prometheus.CounterVec
	PeerSessions             *prometheus.GaugeVec
	PeerDetectTime           *prometheus.GaugeVec
}

var (
	serviceLabels = []string{LabelIface, LabelLocalIP}
)

func withServiceLabels(labels ...string) []string {
	return append(serviceLabels, labels...)
}

func newMetrics() *Metrics {
	return &Metrics{
		Sessions: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "doublezero_liveness_sessions",
				Help: "Current number of sessions by FSM state",
			},
			withServiceLabels(LabelState),
		),
		SessionTransitions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_liveness_session_transitions_total",
				Help: "Count of session state transitions",
			},
			withServiceLabels(LabelStateFrom, LabelStateTo, LabelReason),
		),
		RoutesInstalled: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "doublezero_liveness_routes_installed",
				Help: "Number of routes installed",
			},
			serviceLabels,
		),
		RouteInstalls: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_liveness_route_installs_total",
				Help: "Count of route installs",
			},
			serviceLabels,
		),
		RouteWithdraws: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_liveness_route_withdraws_total",
				Help: "Count of route withdraws",
			},
			serviceLabels,
		),
		ConvergenceToUp: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "doublezero_liveness_convergence_to_up_seconds",
				Help: "Time from first successful control message while down until transition to up (includes detect threshold, scheduler delay, and kernel install).",
			},
			serviceLabels,
		),
		ConvergenceToDown: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "doublezero_liveness_convergence_to_down_seconds",
				Help: "Time from first failed or missing control message while up until transition to down (includes detect expiry, scheduler delay, and kernel delete).",
			},
			serviceLabels,
		),
		SchedulerServiceQueueLen: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "doublezero_liveness_scheduler_service_queue_len",
				Help: "Current number of pending events in the scheduler queue per service (iface, local_ip)",
			},
			serviceLabels,
		),
		SchedulerEventsDropped: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_liveness_scheduler_events_dropped_total",
				Help: "Count of events dropped by the scheduler",
			},
			serviceLabels,
		),
		SchedulerTotalQueueLen: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "doublezero_liveness_scheduler_total_queue_len",
				Help: "Total events currently in the scheduler queue.",
			},
		),
		RouteInstallFailures: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_liveness_route_install_failures_total",
				Help: "Count of route kernel install failures",
			},
			serviceLabels,
		),
		RouteUninstallFailures: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_liveness_route_uninstall_failures_total",
				Help: "Count of route kernel uninstall failures",
			},
			serviceLabels,
		),
		HandleRxDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "doublezero_liveness_handle_rx_duration_seconds",
				Help: "Distribution of time to handle a valid received packet.",
			},
			serviceLabels,
		),
		ControlPacketsTX: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_liveness_control_packets_tx_total",
				Help: "Total control packets sent.",
			},
			serviceLabels, // iface, local_ip
		),
		ControlPacketsRX: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_liveness_control_packets_rx_total",
				Help: "Total control packets received.",
			},
			serviceLabels,
		),
		ControlPacketsRxInvalid: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_liveness_control_packets_rx_invalid_total",
				Help: "Invalid control packets received (e.g. short, bad_version, bad_len, parse_error, not_ipv4, reserved_nonzero).",
			},
			withServiceLabels(LabelReason),
		),
		UnknownPeerPackets: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_liveness_unknown_peer_packets_total",
				Help: "Packets received that didn’t match any known session.",
			},
			serviceLabels,
		),
		ReadSocketErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_liveness_read_socket_errors_total",
				Help: "Count of read socket errors.",
			},
			serviceLabels,
		),
		WriteSocketErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "doublezero_liveness_write_socket_errors_total",
				Help: "Count of write socket errors.",
			},
			serviceLabels,
		),
		// Per peer metrics for route liveness (high cardinality).
		PeerSessions: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "doublezero_liveness_peer_sessions",
				Help: "Current number of sessions by peer and FSM state",
			},
			withServiceLabels(LabelPeerIP, LabelState),
		),
		PeerDetectTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "doublezero_liveness_peer_session_detect_time_seconds",
				Help: "Current detect time by session (after clamping with peer value).",
			},
			withServiceLabels(LabelPeerIP),
		),
	}
}

// Register all metrics with the provided registry.
func (m *Metrics) Register(r prometheus.Registerer) {
	r.MustRegister(
		m.Sessions,
		m.SessionTransitions,
		m.RoutesInstalled,
		m.RouteInstalls,
		m.RouteWithdraws,
		m.ConvergenceToUp,
		m.ConvergenceToDown,
		m.SchedulerServiceQueueLen,
		m.SchedulerEventsDropped,
		m.SchedulerTotalQueueLen,
		m.RouteInstallFailures,
		m.RouteUninstallFailures,
		m.HandleRxDuration,
		m.ControlPacketsTX,
		m.ControlPacketsRX,
		m.ControlPacketsRxInvalid,
		m.UnknownPeerPackets,
		m.ReadSocketErrors,
		m.WriteSocketErrors,
		m.PeerSessions,
		m.PeerDetectTime,
	)
}

func (m *Metrics) sessionStateTransition(peer Peer, prevState *State, newState State, operation string, peerMetrics bool) {
	var prevStateStr string
	if prevState != nil {
		prevStateStr = prevState.String()
	} else {
		prevStateStr = "new"
	}
	newStateStr := newState.String()

	m.SessionTransitions.WithLabelValues(
		peer.Interface, peer.LocalIP, prevStateStr, newStateStr, operation,
	).Inc()

	m.Sessions.WithLabelValues(peer.Interface, peer.LocalIP, newStateStr).Inc()
	if prevState != nil {
		m.Sessions.WithLabelValues(peer.Interface, peer.LocalIP, prevStateStr).Dec()
	}

	if peerMetrics {
		m.PeerSessions.WithLabelValues(peer.Interface, peer.LocalIP, peer.PeerIP, newStateStr).Inc()
		if prevState != nil {
			m.PeerSessions.WithLabelValues(peer.Interface, peer.LocalIP, peer.PeerIP, prevStateStr).Dec()
		}
	}
}

func (m *Metrics) routeInstall(iface, localIP string) {
	m.RoutesInstalled.WithLabelValues(iface, localIP).Inc()
	m.RouteInstalls.WithLabelValues(iface, localIP).Inc()
}

func (m *Metrics) routeWithdraw(iface, localIP string) {
	m.RoutesInstalled.WithLabelValues(iface, localIP).Dec()
	m.RouteWithdraws.WithLabelValues(iface, localIP).Inc()
}

func (m *Metrics) peerDetectTime(peer Peer, dt time.Duration) {
	m.PeerDetectTime.WithLabelValues(peer.Interface, peer.LocalIP, peer.PeerIP).Set(dt.Seconds())
}

func (m *Metrics) cleanupWithdrawRoute(peer Peer, peerMetrics bool) {
	m.Sessions.WithLabelValues(peer.Interface, peer.LocalIP, StateDown.String()).Set(0)
	if peerMetrics {
		m.PeerSessions.DeleteLabelValues(peer.Interface, peer.LocalIP, peer.PeerIP, StateDown.String())
		m.PeerSessions.DeleteLabelValues(peer.Interface, peer.LocalIP, peer.PeerIP, StateInit.String())
		m.PeerSessions.DeleteLabelValues(peer.Interface, peer.LocalIP, peer.PeerIP, StateUp.String())
		m.PeerSessions.DeleteLabelValues(peer.Interface, peer.LocalIP, peer.PeerIP, StateAdminDown.String())
		m.PeerDetectTime.DeleteLabelValues(peer.Interface, peer.LocalIP, peer.PeerIP)
	}
}

func (m *Metrics) convergenceToUp(peer Peer, convergence time.Duration) {
	m.ConvergenceToUp.WithLabelValues(peer.Interface, peer.LocalIP).Observe(convergence.Seconds())
}

func (m *Metrics) convergenceToDown(peer Peer, convergence time.Duration) {
	m.ConvergenceToDown.WithLabelValues(peer.Interface, peer.LocalIP).Observe(convergence.Seconds())
}

func (m *Metrics) schedulerServiceQueueLength(eq *EventQueue, peer Peer) {
	iface, lip := peer.Interface, peer.LocalIP
	cnt := eq.CountFor(iface, lip)
	if cnt == 0 {
		m.SchedulerServiceQueueLen.DeleteLabelValues(iface, lip)
	} else {
		m.SchedulerServiceQueueLen.WithLabelValues(iface, lip).Set(float64(cnt))
	}
}
