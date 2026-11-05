package liveness

import "time"

// DocsMissingPeerRoutes is the troubleshooting page for peer routes that liveness withholds.
const DocsMissingPeerRoutes = "https://docs.doublezero.xyz/support/troubleshooting#issue-missing-peer-routes"

const (
	routeActionGrace      = 2 * time.Minute
	routeActionWarnEvery  = 15 * time.Minute
	routeActionCheckEvery = 10 * time.Second
)

// RouteAction is what the user must unblock for peer routes to be installed.
type RouteAction string

const (
	RouteActionNone RouteAction = "none"
	// RouteActionInbound: this host receives no probes from peers.
	RouteActionInbound RouteAction = "inbound"
	// RouteActionOutbound: this host hears peers, but they never hear it.
	RouteActionOutbound RouteAction = "outbound"
)

var routeActionWarnings = map[RouteAction]string{
	RouteActionInbound:  "liveness: missing peer routes: this host receives no liveness probes from peers; check that inbound UDP 44880 is allowed on doublezero0",
	RouteActionOutbound: "liveness: missing peer routes: peers receive no liveness probes from this host; check that outbound UDP 44880 is allowed on doublezero0",
}

// RouteAction returns the result of the last check, so a status read never runs the rule.
func (m *manager) RouteAction() RouteAction {
	m.actionMu.Lock()
	defer m.actionMu.Unlock()
	return m.action
}

// checkRouteAction applies the rule at now: in active mode, with a route desired,
// action is needed once no session has been Up for routeActionGrace.
func (m *manager) checkRouteAction(now time.Time) {
	if m.cfg.PassiveMode {
		return
	}
	m.actionMu.Lock()
	defer m.actionMu.Unlock()

	m.mu.Lock()
	desired := len(m.desired)
	sessions := make([]SessionSnapshot, 0, len(m.sessions))
	for _, sess := range m.sessions {
		sessions = append(sessions, sess.Snapshot())
	}
	m.mu.Unlock()

	up := false
	for _, s := range sessions {
		up = up || s.State == StateUp
	}
	if desired == 0 || up {
		m.zeroUpSince = time.Time{}
	} else if m.zeroUpSince.IsZero() {
		m.zeroUpSince = now
	}

	action := RouteActionNone
	if !m.zeroUpSince.IsZero() && now.Sub(m.zeroUpSince) >= routeActionGrace {
		action = RouteActionInbound
		for _, s := range sessions {
			// A peer that cannot hear us keeps resetting our session from Init to Down,
			// so any probe since the period began counts, not only a session in Init now.
			if s.State == StateInit || s.LastRx.After(m.zeroUpSince) {
				action = RouteActionOutbound
				break
			}
		}
	}

	switch {
	case action != RouteActionNone && (action != m.action || now.Sub(m.actionWarnedAt) >= routeActionWarnEvery):
		m.actionWarnedAt = now
		m.log.Warn(routeActionWarnings[action],
			"direction", string(action),
			"since", m.zeroUpSince.UTC().String(),
			"routesDesired", desired,
			"docs", DocsMissingPeerRoutes,
		)
	case action == RouteActionNone && m.action != RouteActionNone:
		m.log.Info("liveness: missing peer routes cleared")
	}
	m.action = action
}

// restartRouteActionGrace makes the next check start a new grace period.
func (m *manager) restartRouteActionGrace() {
	m.actionMu.Lock()
	m.zeroUpSince = time.Time{}
	m.actionMu.Unlock()
}
