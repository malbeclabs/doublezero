package api

import (
	"encoding/json"
	"net"
	"net/http"
	"sort"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/liveness"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/config"
	"golang.org/x/sys/unix"
)

type LivenessPeerMode string

const (
	LivenessPeerModeActive  LivenessPeerMode = "active"
	LivenessPeerModePassive LivenessPeerMode = "passive"
)

func (l LivenessPeerMode) String() string {
	return string(l)
}

type Route struct {
	Network                     string `json:"network"`
	LocalIP                     string `json:"local_ip"`
	PeerIP                      string `json:"peer_ip"`
	KernelState                 string `json:"kernel_state,omitempty"`
	LivenessLastUpdated         string `json:"liveness_last_updated,omitempty"`
	LivenessState               string `json:"liveness_state,omitempty"`
	LivenessStateReason         string `json:"liveness_state_reason,omitempty"`
	LivenessExpectedKernelState string `json:"liveness_expected_kernel_state,omitempty"`
	LivenessPeerMode            string `json:"liveness_peer_mode,omitempty"`
	PeerClientVersion           string `json:"peer_client_version,omitempty"`
}

// RouteSummary counts an IBRL service's peer routes and the action the user must take.
type RouteSummary struct {
	Peers        int                  `json:"peers"`
	Installed    int                  `json:"installed"`
	SessionsUp   int                  `json:"sessions_up"`
	NotInstalled []RouteGroup         `json:"not_installed"`
	Action       liveness.RouteAction `json:"action"`
	Docs         string               `json:"docs,omitempty"`
}

// RouteGroup counts missing routes whose sessions share a state and reason.
type RouteGroup struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// SummarizeRoutes checks the desired routes of the sessions sourced from src against the kernel.
func SummarizeRoutes(kernel []*routing.Route, sessions []liveness.SessionSnapshot, src net.IP, action liveness.RouteAction, now time.Time) *RouteSummary {
	inKernel := make(map[routeKey]bool, len(kernel))
	for _, rt := range kernel {
		if rt.Src != nil && rt.Dst != nil {
			inKernel[routeKeyFor(rt)] = true
		}
	}

	sum := &RouteSummary{NotInstalled: []RouteGroup{}, Action: action}
	if action == liveness.RouteActionInbound || action == liveness.RouteActionOutbound {
		sum.Docs = liveness.DocsMissingPeerRoutes
	}
	counts := make(map[RouteGroup]int)
	for _, sess := range sessions {
		rt := &sess.Route.Route
		if sess.ExpectedKernelState == liveness.KernelStateUnknown || rt.Src == nil || rt.Dst == nil || !rt.Src.Equal(src) {
			continue
		}
		sum.Peers++
		if sess.State == liveness.StateUp {
			sum.SessionsUp++
		}
		if inKernel[routeKeyFor(rt)] {
			sum.Installed++
			continue
		}
		counts[RouteGroup{State: sess.State.String(), Reason: livenessStateReason(sess, now)}]++
	}
	for g, n := range counts {
		g.Count = n
		sum.NotInstalled = append(sum.NotInstalled, g)
	}
	sort.Slice(sum.NotInstalled, func(i, j int) bool {
		a, b := sum.NotInstalled[i], sum.NotInstalled[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.State != b.State {
			return a.State < b.State
		}
		return a.Reason < b.Reason
	})
	return sum
}

// livenessStateReason says why a session's route may be missing. It maps here, not in
// DownReason.String(), because log queries filter on that.
func livenessStateReason(sess liveness.SessionSnapshot, now time.Time) string {
	switch {
	case sess.State == liveness.StateDown && sess.LastDownReason == liveness.DownReasonNone:
		return "no reply"
	case sess.State == liveness.StateDown:
		return sess.LastDownReason.String()
	case sess.State == liveness.StateUp && sess.ExpectedKernelState == liveness.KernelStateAbsent && now.Before(sess.HoldDownUntil):
		return "held down"
	}
	return ""
}

type routeKey struct {
	Src     string
	Dst     string
	NextHop string
}

func routeKeyFor(rt *routing.Route) routeKey {
	return routeKey{
		Src:     rt.Src.To4().String(),
		Dst:     rt.Dst.IP.To4().String(),
		NextHop: rt.NextHop.To4().String(),
	}
}

type LivenessManager interface {
	GetSessions() []liveness.SessionSnapshot
}

type ServiceStateReader interface {
	GetProvisionedServices() []*ProvisionRequest
}

func ServeRoutesHandler(nlr bgp.RouteReaderWriter, lm LivenessManager, ssr ServiceStateReader, networkConfig *config.NetworkConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rts, err := nlr.RouteByProtocol(unix.RTPROT_BGP)
		if err != nil {
			http.Error(w, "failed to get routes", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		services := ssr.GetProvisionedServices()

		// Get the routes from the kernel, filtered by doublezero services.
		kernelRoutes := make(map[routeKey]*Route, len(rts))
		for _, rt := range rts {
			if rt.Src == nil || rt.Dst == nil || rt.Src.To4() == nil || rt.Dst.IP.To4() == nil {
				continue
			}
			for _, svc := range services {
				if svc.DoubleZeroIP == nil || svc.TunnelSrc == nil || svc.TunnelNet == nil || svc.TunnelNet.IP == nil {
					continue
				}
				if svc.DoubleZeroIP.Equal(rt.Src) && svc.TunnelNet.IP.Equal(rt.NextHop) {
					kernelRoutes[routeKeyFor(rt)] = &Route{
						Network:     networkConfig.Moniker,
						LocalIP:     rt.Src.To4().String(),
						PeerIP:      rt.Dst.IP.To4().String(),
						KernelState: liveness.KernelStatePresent.String(),
					}
					break
				}
			}
		}

		// If the liveness manager is enabled, we need to get the routes from the liveness manager.
		livenessRoutes := make(map[routeKey]*Route, 0)
		if lm != nil {
			now := time.Now()
			sessions := lm.GetSessions()
			livenessRoutes = make(map[routeKey]*Route, len(sessions))
			for _, sess := range sessions {
				rt := &sess.Route
				if rt.Src == nil || rt.Dst == nil || rt.Src.To4() == nil || rt.Dst.IP.To4() == nil {
					continue
				}
				for _, svc := range services {
					if svc.DoubleZeroIP == nil || svc.TunnelSrc == nil || svc.TunnelNet == nil || svc.TunnelNet.IP == nil {
						continue
					}
					if !svc.DoubleZeroIP.Equal(rt.Src) || !svc.TunnelNet.IP.Equal(rt.NextHop) {
						continue
					}

					rk := routeKeyFor(&rt.Route)
					kernelState := liveness.KernelStateAbsent.String()
					if _, ok := kernelRoutes[rk]; ok {
						kernelState = liveness.KernelStatePresent.String()
					}

					livenessRoutes[rk] = &Route{
						Network:                     networkConfig.Moniker,
						LocalIP:                     rt.Src.To4().String(),
						PeerIP:                      rt.Dst.IP.To4().String(),
						KernelState:                 kernelState,
						LivenessLastUpdated:         sess.LastUpdated.UTC().Format(time.RFC3339),
						LivenessState:               sess.State.String(),
						LivenessStateReason:         livenessStateReason(sess, now),
						LivenessExpectedKernelState: sess.ExpectedKernelState.String(),
						LivenessPeerMode:            sess.PeerAdvertisedMode.String(),
						PeerClientVersion:           sess.PeerClientVersion.String(),
					}
					break
				}
			}
		}

		// Merge kernel and liveness routes.
		routes := make([]*Route, 0, max(len(livenessRoutes), len(kernelRoutes)))
		for _, rt := range livenessRoutes {
			routes = append(routes, rt)
		}
		for rk, rt := range kernelRoutes {
			if _, ok := livenessRoutes[rk]; !ok {
				routes = append(routes, rt)
			}
		}

		// Sort for consistent ordering in the API response.
		sort.Slice(routes, func(i, j int) bool {
			a, b := routes[i], routes[j]
			if a.Network != b.Network {
				return a.Network < b.Network
			}
			if a.LocalIP != b.LocalIP {
				return a.LocalIP < b.LocalIP
			}
			if a.PeerIP != b.PeerIP {
				return a.PeerIP < b.PeerIP
			}
			if a.KernelState != b.KernelState {
				return a.KernelState < b.KernelState
			}
			if a.LivenessState != b.LivenessState {
				return a.LivenessState < b.LivenessState
			}
			return false
		})

		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(routes); err != nil {
			http.Error(w, "failed to encode routes", http.StatusInternalServerError)
			return
		}
	}
}
