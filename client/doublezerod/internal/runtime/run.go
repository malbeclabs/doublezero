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
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"golang.org/x/sys/unix"
)

func Run(ctx context.Context, sockFile string, routeConfigPath string, enableLatencyProbing, enableLatencyMetrics bool, programId string, rpcEndpoint string, probeInterval, cacheUpdateInterval int) error {
	nlr := routing.Netlink{}
	var crw bgp.RouteReaderWriter
	if _, err := os.Stat(routeConfigPath); os.IsNotExist(err) {
		crw = nlr
	} else {
		crw, err = routing.NewConfiguredRouteReaderWriter(slog.Default(), nlr, routeConfigPath)
		if err != nil {
			return fmt.Errorf("error creating configured route reader writer: %v", err)
		}
	}

	// TODO(snormore): Move this up into main.go and make it configurable via CLI flags.
	// TODO(snormore): This needs to support passive-mode where protocol functions but kernel
	// routing table is not managed, for phase 1 of the rollout.
	lm, err := liveness.NewManager(ctx, &liveness.ManagerConfig{
		Logger:    slog.Default(),
		Netlinker: crw,
		BindIP:    "0.0.0.0",
		Port:      44880,

		TxMin:      300 * time.Millisecond,
		RxMin:      300 * time.Millisecond,
		DetectMult: 3,
		MinTxFloor: 50 * time.Millisecond,
		MaxTxCeil:  1 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("error creating liveness manager: %v", err)
	}
	defer lm.Close()

	bgp, err := bgp.NewBgpServer(net.IPv4(1, 1, 1, 1), crw, lm)
	if err != nil {
		return fmt.Errorf("error creating bgp server: %v", err)
	}

	db, err := manager.NewDb()
	if err != nil {
		return fmt.Errorf("error initializing db: %v", err)
	}

	pim := pim.NewPIMServer()
	nlm := manager.NewNetlinkManager(nlr, bgp, db, pim)

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

	if enableLatencyProbing {
		latencyManager := latency.NewLatencyManager(
			latency.WithProgramID(programId),
			latency.WithRpcEndpoint(rpcEndpoint),
			latency.WithProbeInterval(time.Duration(probeInterval)*time.Second),
			latency.WithCacheUpdateInterval(time.Duration(cacheUpdateInterval)*time.Second),
			latency.WithMetricsEnabled(enableLatencyMetrics),
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
			"program_id": programId,
			"rpc_url":    rpcEndpoint,
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

	select {
	case <-ctx.Done():
		slog.Info("teardown: cleaning up and closing")
		nlm.Close()
		api.Close()
		return nil
	case err := <-errCh:
		return err
	}
}
