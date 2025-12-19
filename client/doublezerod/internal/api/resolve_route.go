package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/config"
)

type ResolveRouteRequest struct {
	Dst net.IP
}

type ResolveRouteResponse struct {
	Src net.IP
}

func (r *ResolveRouteRequest) Validate() error {
	if r.Dst == nil {
		return errors.New("dst is required")
	}
	return nil
}

func ServeResolveRouteHandler(nlr routing.Netlinker, networkConfig *config.NetworkConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ResolveRouteRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, "malformed request", http.StatusBadRequest)
			return
		}
		if err = req.Validate(); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		routes, err := nlr.RouteGet(req.Dst)
		if err != nil {
			http.Error(w, "failed to resolve route", http.StatusInternalServerError)
			return
		}
		for _, route := range routes {
			if route.Dst.IP.Equal(req.Dst) {
				if err := json.NewEncoder(w).Encode(ResolveRouteResponse{Src: route.Src}); err != nil {
					http.Error(w, "failed to encode response", http.StatusInternalServerError)
					return
				}
				return
			}
		}
		http.Error(w, "route not found", http.StatusNotFound)
	}
}
