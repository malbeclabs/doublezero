// Package reconcile provides periodic kernel route reconciliation for
// doublezerod, independent of the route-liveness subsystem.
//
// The Reconciler is a transparent routing.Netlinker decorator that sits at the
// base of the route reader/writer chain (just above raw netlink). It records
// every BGP route actually pushed to the kernel and, on a configurable tick,
// reinstalls any that have gone missing (e.g. deleted by another process or an
// administrator). Because every kernel route write in the daemon bottoms out at
// this layer — whether driven by route liveness (passive or active) or by the
// BGP server directly when liveness is disabled — reconciliation works in all
// of those configurations without depending on the liveness manager.
//
// Layering matters for exclusions: the Reconciler must sit *below*
// routing.ConfiguredRouteReaderWriter, whose RouteAdd no-ops for excluded
// destinations. Excluded routes then never reach this layer, are never
// tracked, and are never falsely reinstalled.
package reconcile

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sys/unix"
)

// routeKey identifies a route for matching the kernel against the tracked set.
// Dst carries the full prefix (IP + mask) so a kernel route with a different
// mask does not satisfy a tracked route at the same IP (e.g. 10.0.0.0/16 must
// not match a tracked 10.0.0.0/24). IPs are normalized to their 4-byte form so
// a 16-byte net.IP and its 4-byte equivalent compare equal on both sides.
type routeKey struct {
	Table   int
	Dst     string
	NextHop string
	SrcIP   string
}

func ipString(ip net.IP) string {
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func dstString(n *net.IPNet) string {
	if n == nil || n.IP == nil {
		return ""
	}
	return n.String()
}

func keyFor(r *routing.Route) routeKey {
	return routeKey{
		Table:   r.Table,
		Dst:     dstString(r.Dst),
		NextHop: ipString(r.NextHop),
		SrcIP:   ipString(r.Src),
	}
}

// Reconciler decorates a routing.Netlinker, tracking installed BGP routes and
// periodically reinstalling any that go missing from the kernel.
type Reconciler struct {
	// Embedded Netlinker provides the full routing.Netlinker surface (tunnels,
	// rules, etc.) by promotion; only RouteAdd, RouteDelete, and TunnelDelete
	// are overridden below.
	routing.Netlinker

	log      *slog.Logger
	interval time.Duration
	metrics  *metrics

	mu      sync.Mutex
	tracked map[routeKey]*routing.Route

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a Reconciler wrapping inner. interval <= 0 means reconciliation is
// disabled (the decorator still tracks routes, but Start launches no ticker). If
// reg is nil, metrics register with prometheus.DefaultRegisterer.
func New(log *slog.Logger, inner routing.Netlinker, interval time.Duration, reg prometheus.Registerer) *Reconciler {
	if log == nil {
		log = slog.Default()
	}
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	return &Reconciler{
		Netlinker: inner,
		log:       log,
		interval:  interval,
		metrics:   newMetrics(reg),
		tracked:   make(map[routeKey]*routing.Route),
	}
}

// RouteAdd installs r via the inner Netlinker and, for BGP routes, records it in
// the tracked set on success so reconciliation can reinstall it if it later
// disappears from the kernel. Non-BGP routes (e.g. multicast RTPROT_STATIC
// mroutes) pass through untracked.
func (rc *Reconciler) RouteAdd(r *routing.Route) error {
	if r.Protocol != unix.RTPROT_BGP {
		return rc.Netlinker.RouteAdd(r)
	}
	if err := rc.Netlinker.RouteAdd(r); err != nil {
		return err
	}
	rc.mu.Lock()
	rc.tracked[keyFor(r)] = r
	rc.mu.Unlock()
	return nil
}

// RouteDelete removes the route from the tracked set under the lock *before*
// issuing the kernel delete, so a concurrent reconcile tick cannot resurrect a
// route that is being withdrawn.
//
// Untracking is deliberately protocol-agnostic: BGP withdraw-driven deletes are
// constructed without a Protocol (bgp/plugin.go builds them from the withdraw
// NLRI), so filtering on RTPROT_BGP here would leak those entries and the
// reconciler would resurrect withdrawn routes. Deleting an untracked key is a
// no-op, so passing every delete through the untrack path is safe.
func (rc *Reconciler) RouteDelete(r *routing.Route) error {
	rc.mu.Lock()
	delete(rc.tracked, keyFor(r))
	rc.mu.Unlock()
	return rc.Netlinker.RouteDelete(r)
}

// TunnelDelete purges tracked routes whose next hop is the tunnel's remote
// overlay address before deleting the tunnel. The kernel drops routes with
// their link, but teardown paths that skip route withdrawal (NoUninstall, used
// by IBRL-with-allocated-IP) never issue a RouteDelete through this layer; the
// purge mirrors the kernel's own behavior so the reconciler does not try to
// reinstall routes onto a deleted tunnel forever.
func (rc *Reconciler) TunnelDelete(t *routing.Tunnel) error {
	if t != nil && t.RemoteOverlay != nil {
		nh := ipString(t.RemoteOverlay)
		rc.mu.Lock()
		for k := range rc.tracked {
			if k.NextHop == nh {
				delete(rc.tracked, k)
			}
		}
		rc.mu.Unlock()
	}
	return rc.Netlinker.TunnelDelete(t)
}

// Start launches the reconciliation ticker if interval > 0. It is a no-op
// otherwise. Stop cancels the ticker and waits for it to exit.
func (rc *Reconciler) Start(ctx context.Context) {
	if rc.interval <= 0 {
		rc.log.Info("route reconcile: disabled")
		return
	}
	rc.log.Info("route reconcile: enabled", "interval", rc.interval.String())

	ctx, cancel := context.WithCancel(ctx)
	rc.cancel = cancel
	rc.wg.Go(func() {
		ticker := time.NewTicker(rc.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rc.reconcile()
			}
		}
	})
}

// Stop cancels the ticker goroutine and waits for it to exit.
func (rc *Reconciler) Stop() {
	if rc.cancel != nil {
		rc.cancel()
	}
	rc.wg.Wait()
}

// reconcile scans the kernel BGP routing table for tracked routes that have gone
// missing and reinstalls them.
//
// NOTE: routing.Netlink.RouteByProtocol only returns main-table routes (see the
// NOTE on that method); tracked routes in other tables would be declared
// missing every tick. All current BGP route writers use RT_TABLE_MAIN.
func (rc *Reconciler) reconcile() {
	rc.mu.Lock()
	toCheck := make([]*routing.Route, 0, len(rc.tracked))
	for _, r := range rc.tracked {
		toCheck = append(toCheck, r)
	}
	rc.mu.Unlock()

	if len(toCheck) == 0 {
		return
	}

	kernelRoutes, err := rc.Netlinker.RouteByProtocol(unix.RTPROT_BGP)
	if err != nil {
		rc.log.Error("route reconcile: error fetching kernel routes", "error", err)
		return
	}

	kernelSet := make(map[routeKey]struct{}, len(kernelRoutes))
	for _, kr := range kernelRoutes {
		kernelSet[keyFor(kr)] = struct{}{}
	}

	for _, r := range toCheck {
		if _, present := kernelSet[keyFor(r)]; present {
			continue
		}
		// Re-check and reinstall under the lock. RouteDelete removes the key
		// under rc.mu *before* issuing its kernel delete, so holding the lock
		// across the re-check and RouteAdd closes the resurrection race: either
		// we observe the withdrawal and skip, or our add completes before the
		// delete lands. The netlink call under the lock only happens for
		// genuinely-missing routes, which are rare by definition.
		rc.mu.Lock()
		k := keyFor(r)
		if _, still := rc.tracked[k]; !still {
			rc.mu.Unlock()
			continue
		}
		err := rc.Netlinker.RouteAdd(r)
		rc.mu.Unlock()

		localIP := ipString(r.Src)
		if err != nil {
			rc.log.Error("route reconcile: error reinstalling route", "error", err, "route", r.String())
			rc.metrics.failures.WithLabelValues(localIP).Inc()
			continue
		}
		rc.log.Warn("route reconcile: reinstalled missing route", "route", r.String())
		rc.metrics.reinstalls.WithLabelValues(localIP).Inc()
	}
}
