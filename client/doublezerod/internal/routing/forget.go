package routing

import "net"

// RouteForgetter is implemented by route writers that remember the routes they
// installed and restore them if they go missing (reconcile.Reconciler).
// Withdrawals that deliberately leave a kernel route in place, or that cannot
// see a route because it is already gone from the kernel, use it to stop that
// route from being restored. Wrappers forward it with ForgetRoute and
// ForgetRoutesVia.
type RouteForgetter interface {
	// RouteForget stops remembering r without touching the kernel.
	RouteForget(r *Route)
	// RouteForgetVia stops remembering every route via nextHop without touching
	// the kernel.
	RouteForgetVia(nextHop net.IP)
}

// ForgetRoute calls w.RouteForget if w is a RouteForgetter.
func ForgetRoute(w any, r *Route) {
	if f, ok := w.(RouteForgetter); ok {
		f.RouteForget(r)
	}
}

// ForgetRoutesVia calls w.RouteForgetVia if w is a RouteForgetter.
func ForgetRoutesVia(w any, nextHop net.IP) {
	if f, ok := w.(RouteForgetter); ok {
		f.RouteForgetVia(nextHop)
	}
}
