package liveness

import (
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"golang.org/x/sys/unix"
)

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
	// RouteActionStarting: inbound or outbound would apply, but routeActionGrace has not passed.
	RouteActionStarting RouteAction = "starting"
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

// checkRouteAction applies the rule at now: in active mode, action is needed once no
// session has been Up, and a desired route missing from the kernel, for routeActionGrace.
func (m *manager) checkRouteAction(now time.Time) {
	if m.cfg.PassiveMode {
		return
	}
	m.actionMu.Lock()
	defer m.actionMu.Unlock()

	m.mu.Lock()
	desired := make([]RouteKey, 0, len(m.desired))
	for _, r := range m.desired {
		desired = append(desired, kernelKey(&r.Route))
	}
	sessions := make([]SessionSnapshot, 0, len(m.sessions))
	for _, sess := range m.sessions {
		sessions = append(sessions, sess.Snapshot())
	}
	m.mu.Unlock()

	up := false
	for _, s := range sessions {
		up = up || s.State == StateUp
	}
	// The kernel, not installed[], because onSessionDown clears NoUninstall routes it keeps.
	missing := 0
	if !up && len(desired) > 0 {
		kernel, err := m.cfg.Netlinker.RouteByProtocol(unix.RTPROT_BGP)
		if err != nil {
			m.log.Warn("liveness: failed to list kernel routes for the missing peer routes check", "error", err)
			return
		}
		inKernel := make(map[RouteKey]bool, len(kernel))
		for _, r := range kernel {
			if r.Src != nil && r.Dst != nil {
				inKernel[kernelKey(r)] = true
			}
		}
		for _, k := range desired {
			if !inKernel[k] {
				missing++
			}
		}
	}
	if up || missing == 0 {
		m.zeroUpSince = time.Time{}
	} else if m.zeroUpSince.IsZero() {
		m.zeroUpSince = now
	}

	action := RouteActionNone
	switch {
	case m.zeroUpSince.IsZero():
	case now.Sub(m.zeroUpSince) < routeActionGrace:
		action = RouteActionStarting
	default:
		action = RouteActionInbound
		for _, s := range sessions {
			// A peer that cannot hear us keeps resetting our session from Init to Down, so count
			// any probe in the last routeActionGrace, which outlasts the default 60 s Down probe interval.
			if s.LastRx.After(now.Add(-routeActionGrace)) {
				action = RouteActionOutbound
				break
			}
		}
	}

	msg, needed := routeActionWarnings[action]
	_, wasNeeded := routeActionWarnings[m.action]
	switch {
	case needed && (action != m.action || now.Sub(m.actionWarnedAt) >= routeActionWarnEvery):
		m.actionWarnedAt = now
		m.log.Warn(msg,
			"direction", string(action),
			"since", m.zeroUpSince.UTC().String(),
			"peers", len(desired),
			"docs", DocsMissingPeerRoutes,
		)
	case !needed && wasNeeded:
		m.log.Info("liveness: missing peer routes cleared")
	}
	m.action = action
}

// kernelKey matches routes by source, destination and next hop, as the route summary does.
func kernelKey(r *routing.Route) RouteKey {
	return RouteKey{SrcIP: r.Src.To4().String(), DstPrefix: r.Dst.IP.To4().String(), NextHop: r.NextHop.To4().String()}
}

// restartRouteActionGrace makes the next check start a new grace period.
func (m *manager) restartRouteActionGrace() {
	m.actionMu.Lock()
	m.zeroUpSince = time.Time{}
	m.actionMu.Unlock()
}
