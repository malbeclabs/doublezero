package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/latency"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/liveness"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/manager"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/multicast"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/config"
	"golang.org/x/sys/unix"
)

const (
	updateInstalledRoutesGaugeInterval = 10 * time.Second
)

func Run(ctx context.Context, sockFile string, routeConfigPath string, enableLatencyProbing, enableLatencyMetrics, latencyProbeTunnelEndpoints bool, networkConfig *config.NetworkConfig, probeInterval, cacheUpdateInterval int, lmc *liveness.ManagerConfig) error {
	nlr := routing.Netlink{}
	var crw bgp.RouteReaderWriter
	var cr *routing.ConfiguredRoutes
	if _, err := os.Stat(routeConfigPath); os.IsNotExist(err) {
		crw = nlr
	} else {
		cr, err = routing.NewConfiguredRoutes(routeConfigPath)
		if err != nil {
			return fmt.Errorf("error creating configured routes: %v", err)
		}
		crw, err = routing.NewConfiguredRouteReaderWriter(slog.Default(), nlr, cr)
		if err != nil {
			return fmt.Errorf("error creating configured route reader writer: %v", err)
		}
	}

	// If the liveness manager config is not nil, create a new manager.
	// Otherwise, completely disable the liveness subsystem.
	// TODO(snormore): The scenario where the liveness subsystem is completely disabled is
	// temporary for initial rollout testing.
	var lm liveness.Manager
	if lmc != nil {
		lmc.Netlinker = crw
		var err error
		lm, err = liveness.NewManager(ctx, lmc, cr)
		if err != nil {
			return fmt.Errorf("error creating liveness manager: %v", err)
		}
		defer lm.Close()
	}

	bgp, err := bgp.NewBgpServer(net.IPv4(1, 1, 1, 1), crw, lm)
	if err != nil {
		return fmt.Errorf("error creating bgp server: %v", err)
	}

	db, err := manager.NewDb()
	if err != nil {
		return fmt.Errorf("error initializing db: %v", err)
	}

	pim := pim.NewPIMServer()
	heartbeat := multicast.NewHeartbeatSender()
	nlm := manager.NewNetlinkManager(nlr, bgp, db, pim, heartbeat)

	errCh := make(chan error)

	// starting network manager will attempt to recover latest provisioned state
	slog.Info("network: starting network manager")
	go func() {
		err := nlm.Serve(ctx)
		errCh <- err
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /provision", nlm.ServeProvision)
	mux.HandleFunc("POST /remove", nlm.ServeRemove)
	mux.HandleFunc("GET /status", nlm.ServeStatus)
	mux.HandleFunc("GET /routes", api.ServeRoutesHandler(nlr, lm, db, networkConfig))
	mux.HandleFunc("POST /resolve-route", api.ServeResolveRouteHandler(nlr, networkConfig))

	if enableLatencyProbing {
		latencyManager := latency.NewLatencyManager(
			latency.WithProgramID(networkConfig.ServiceabilityProgramID.String()),
			latency.WithRpcEndpoint(networkConfig.LedgerPublicRPCURL),
			latency.WithProbeInterval(time.Duration(probeInterval)*time.Second),
			latency.WithCacheUpdateInterval(time.Duration(cacheUpdateInterval)*time.Second),
			latency.WithMetricsEnabled(enableLatencyMetrics),
			latency.WithProbeTunnelEndpoints(latencyProbeTunnelEndpoints),
		)
		go func() {
			err := latencyManager.Start(ctx)
			errCh <- err
		}()
		mux.HandleFunc("GET /latency", latencyManager.ServeLatency)
	}

	// /config endpoint returns:
	// {
	//   "program_id": "<string>", // The program ID used by the client
	//   "rpc_url": "<string>"     // The RPC endpoint URL
	// }
	mux.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]string{
			"program_id": networkConfig.ServiceabilityProgramID.String(),
			"rpc_url":    networkConfig.LedgerPublicRPCURL,
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
		}
	})

	opts := []api.Option{
		api.WithBaseContext(ctx),
		api.WithHandler(mux),
	}
	if sockFile != "" {
		opts = append(opts, api.WithSockFile(sockFile))
	}

	api := api.NewApiServer(opts...)

	lis, err := net.Listen("unix", sockFile)
	if err != nil {
		return fmt.Errorf("error creating listener: %v", err)
	}
	defer unix.Unlink(sockFile) //nolint

	err = os.Chmod(sockFile, 0666)
	if err != nil {
		slog.Error("error setting socket file perms", "error", err)
	}

	slog.Info("http: starting api manager")
	go func() {
		err := api.Serve(lis)
		errCh <- err
	}()

	go updateInstalledRoutesGauge(ctx, nlr)

	// The liveness manager can be nil if the liveness subsystem is disabled.
	// TODO(snormore): The scenario where the liveness subsystem is completely disabled is
	// temporary for initial rollout testing.
	var lmErrCh <-chan error
	if lm != nil {
		lmErrCh = lm.Err()
	}

	select {
	case <-ctx.Done():
		slog.Info("teardown: cleaning up and closing")
		nlm.Close()
		api.Close()
		return nil
	case err := <-errCh:
		return err
	case err := <-lmErrCh:
		return err
	}
}

func updateInstalledRoutesGauge(ctx context.Context, nlr routing.Netlinker) {
	tick := func() {
		routes, err := nlr.RouteByProtocol(unix.RTPROT_BGP)
		if err != nil {
			slog.Error("runtime: error listing kernel bgp routes", "error", err)
			return
		}

		routesBySrcNextHop := make(map[string]map[string]int)
		for _, route := range routes {
			if route.Protocol != unix.RTPROT_BGP {
				continue
			}
			if route.Src == nil || route.NextHop == nil || route.Src.To4() == nil || route.NextHop.To4() == nil {
				continue
			}
			src := route.Src.To4().String()
			nextHop := route.NextHop.To4().String()
			if _, ok := routesBySrcNextHop[src]; !ok {
				routesBySrcNextHop[src] = make(map[string]int)
			}
			routesBySrcNextHop[src][nextHop]++
		}

		metricBGPRoutesInstalled.Reset()
		for src, nextHops := range routesBySrcNextHop {
			for nextHop, count := range nextHops {
				metricBGPRoutesInstalled.WithLabelValues(src, nextHop).Set(float64(count))
			}
		}
	}

	tick()
	ticker := time.NewTicker(updateInstalledRoutesGaugeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick()
		}
	}
}
