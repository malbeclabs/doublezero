//go:build linux

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
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/latency"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/manager"
	"golang.org/x/sys/unix"
)

func Run(ctx context.Context, nlm *manager.NetlinkManager, sockFile string, enableLatencyProbing, enableLatencyMetrics bool, programId string, rpcEndpoint string, probeInterval, cacheUpdateInterval int) error {
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
