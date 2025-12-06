package liveness

import (
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"golang.org/x/sys/unix"
)

// RouteReaderWriter is the minimal interface for interacting with the routing
// backend. It allows adding, deleting, and listing routes by protocol.
// The BGP plugin uses this to interact with the kernel routing table through
// the liveness subsystem, without depending on its internal implementation.
type RouteReaderWriter interface {
	RouteAdd(*routing.Route) error
	RouteDelete(*routing.Route) error
	RouteByProtocol(int) ([]*routing.Route, error)
}

// routeReaderWriter is an interface-specific adapter that connects a single
// network interface (iface) to the liveness Manager. It is typically created
// by the BGP plugin so that each managed interface has its own scoped view
// of route registration and withdrawal through the Manager.
type routeReaderWriter struct {
	lm          Manager           // liveness manager handling route lifecycle
	rrw         RouteReaderWriter // underlying netlink backend
	iface       string            // interface name associated with these routes
	port        int               // liveness port associated with these routes
	noUninstall bool              // if true, the route will not be uninstalled from the kernel on route withdrawal
}

// NewRouteReaderWriter creates an interface-scoped RouteReaderWriter that
// wraps the liveness Manager and a concrete routing backend. This allows the
// BGP plugin to use standard routing calls while the Manager tracks route
// liveness on a per-interface basis.
func NewRouteReaderWriter(lm Manager, rrw RouteReaderWriter, iface string, noUninstall bool) *routeReaderWriter {
	return &routeReaderWriter{
		lm:          lm,
		rrw:         rrw,
		iface:       iface,
		port:        DefaultLivenessPort,
		noUninstall: noUninstall,
	}
}

// RouteAdd registers the route with the liveness Manager for the given iface,
// enabling the Manager to monitor reachability before installation.
func (m *routeReaderWriter) RouteAdd(r *routing.Route) error {
	return m.lm.RegisterRoute(&Route{Route: *r, NoUninstall: m.noUninstall}, m.iface, m.port)
}

// RouteDelete withdraws the route and removes it from liveness tracking for
// the associated interface.
func (m *routeReaderWriter) RouteDelete(r *routing.Route) error {
	return m.lm.WithdrawRoute(&Route{Route: *r, NoUninstall: m.noUninstall}, m.iface)
}

// RouteByProtocol delegates to the underlying backend to list routes by
// protocol ID without involving the Manager.
func (m *routeReaderWriter) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	if protocol != unix.RTPROT_BGP {
		return m.rrw.RouteByProtocol(protocol)
	}

	// Return all routes that we are tracking, whether installed in the kernel or not.
	// This method is used to flush routes when a BGP session is closed, so we need to  return
	// all routes that need to be cleaned up.
	routes := []*routing.Route{}
	for _, sess := range m.lm.GetSessions() {
		routes = append(routes, &sess.Route.Route)
	}
	return routes, nil
}
