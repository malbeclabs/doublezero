package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/config"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/latency"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/manager"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	networkconfig "github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"golang.org/x/sys/unix"
)

const (
	defaultNetworkEnv = networkconfig.EnvTestnet
)

func Run(ctx context.Context, log *slog.Logger, sockFile string, enableLatencyProbing bool, configPath string, probeInterval, cacheUpdateInterval int) error {
	nlr := routing.Netlink{}
	bgp, err := bgp.NewBgpServer(net.IPv4(1, 1, 1, 1), nlr)
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
	log.Info("network: starting network manager")
	go func() {
		err := nlm.Serve(ctx)
		errCh <- err
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /provision", nlm.ServeProvision)
	mux.HandleFunc("POST /remove", nlm.ServeRemove)
	mux.HandleFunc("GET /status", nlm.ServeStatus)

	// If the config file does not exist, create it with default network config.
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Info("Config file does not exist, creating default config", "path", configPath)

		networkConfig, err := networkconfig.NetworkConfigForEnv(defaultNetworkEnv)
		if err != nil {
			log.Error("Failed to get network config", "error", err)
			os.Exit(1)
		}

		cfg := config.New(configPath)
		_, err = cfg.Update(networkConfig.LedgerPublicRPCURL, networkConfig.ServiceabilityProgramID)
		if err != nil {
			return fmt.Errorf("error creating default config: %v", err)
		}
	}

	// Load the config file.
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("error loading config: %v", err)
	} else {
		log.Info("Loaded config", "path", configPath)
	}

	if enableLatencyProbing {
		latency, err := latency.NewManager(latency.Config{
			Logger: log,
			Config: cfg,
			NewServiceabilityClientFunc: func(rpcURL string, programID solana.PublicKey) latency.ServiceabilityClient {
				rpcClient := solanarpc.New(rpcURL)
				return serviceability.New(rpcClient, programID)
			},
		})
		if err != nil {
			return fmt.Errorf("error creating latency manager: %v", err)
		}
		go func() {
			err := latency.Start(ctx)
			errCh <- err
		}()
		mux.HandleFunc("GET /latency", latency.ServeLatency)
	}

	mux.HandleFunc("PUT /config", config.NewUpdateHandler(log, cfg))

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
		log.Error("error setting socket file perms", "error", err)
	}

	log.Info("http: starting api manager")
	go func() {
		err := api.Serve(lis)
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		log.Info("teardown: cleaning up and closing")
		nlm.Close()
		api.Close()
		return nil
	case err := <-errCh:
		return err
	}
}
