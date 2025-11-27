package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/liveness"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/config"
	"golang.org/x/sys/unix"
)

type KernelState string

const (
	KernelStateUnknown KernelState = "unknown"
	KernelStatePresent KernelState = "present"
	KernelStateAbsent  KernelState = "absent"
)

func (r KernelState) String() string {
	return string(r)
}

type Route struct {
	Network             string      `json:"network"`
	UserType            UserType    `json:"user_type"`
	LocalIP             string      `json:"local_ip"`
	PeerIP              string      `json:"peer_ip"`
	KernelState         KernelState `json:"kernel_state"`
	LivenessLastUpdated string      `json:"liveness_last_updated,omitempty"`
	LivenessState       string      `json:"liveness_state,omitempty"`
	LivenessStateReason string      `json:"liveness_state_reason,omitempty"`
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

type DBReader interface {
	GetState(userTypes ...UserType) []*ProvisionRequest
}

func ServeRoutesHandler(nlr bgp.RouteReaderWriter, lm LivenessManager, db DBReader, networkConfig *config.NetworkConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rts, err := nlr.RouteByProtocol(unix.RTPROT_BGP)
		if err != nil {
			http.Error(w, "failed to get routes", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		services := db.GetState()

		// Get the routes from the kernel, filtered by doublezero services.
		kernelRoutes := make(map[routeKey]*Route, len(rts))
		for _, rt := range rts {
			if rt.Src == nil || rt.Dst == nil || rt.Src.To4() == nil || rt.Dst.IP.To4() == nil {
				continue
			}
			for _, svc := range services {
				if svc.TunnelSrc == nil || svc.TunnelNet == nil || svc.TunnelNet.IP == nil {
					continue
				}
				if svc.TunnelSrc.Equal(rt.Src) && svc.TunnelNet.IP.Equal(rt.NextHop) {
					kernelRoutes[routeKeyFor(rt)] = &Route{
						Network:     networkConfig.Moniker,
						UserType:    svc.UserType,
						LocalIP:     rt.Src.To4().String(),
						PeerIP:      rt.Dst.IP.To4().String(),
						KernelState: KernelStatePresent,
					}
					break
				}
			}
		}

		// If the liveness manager is enabled, we need to get the routes from the liveness manager.
		livenessRoutes := make(map[routeKey]*Route, 0)
		if lm != nil {
			sessions := lm.GetSessions()
			livenessRoutes = make(map[routeKey]*Route, len(sessions))
			for _, sess := range sessions {
				rt := &sess.Route
				if rt.Src == nil || rt.Dst == nil || rt.Src.To4() == nil || rt.Dst.IP.To4() == nil {
					continue
				}
				for _, svc := range services {
					if svc.TunnelSrc == nil || svc.TunnelNet == nil || svc.TunnelNet.IP == nil {
						continue
					}
					if !svc.TunnelSrc.Equal(rt.Src) || !svc.TunnelNet.IP.Equal(rt.NextHop) {
						continue
					}

					rk := routeKeyFor(rt)
					kernelState := KernelStateAbsent
					if _, ok := kernelRoutes[rk]; ok {
						kernelState = KernelStatePresent
					}

					var stateReason string
					if sess.State == liveness.StateDown {
						stateReason = sess.LastDownReason.String()
					}

					livenessRoutes[rk] = &Route{
						Network:             networkConfig.Moniker,
						UserType:            svc.UserType,
						LocalIP:             rt.Src.To4().String(),
						PeerIP:              rt.Dst.IP.To4().String(),
						KernelState:         kernelState,
						LivenessLastUpdated: sess.LastUpdated.UTC().Format(time.RFC3339),
						LivenessState:       sess.State.String(),
						LivenessStateReason: stateReason,
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
