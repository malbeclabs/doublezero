package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/config"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/runtime"
	networkconfig "github.com/malbeclabs/doublezero/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultSockFile   = "/var/run/doublezerod/doublezerod.sock"
	defaultConfigPath = "/var/run/doublezerod/config.json"
)

var (
	sockFile             = flag.String("sock-file", defaultSockFile, "path to doublezerod domain socket")
	configPath           = flag.String("config", defaultConfigPath, "path to doublezerod config file")
	enableLatencyProbing = flag.Bool("latency-probing", true, "enable latency probing to doublezero nodes")
	versionFlag          = flag.Bool("version", false, "build version")
	programId            = flag.String("program-id", networkconfig.TestnetServiceabilityProgramID, "override smartcontract program id to monitor")
	rpcEndpoint          = flag.String("solana-rpc-endpoint", networkconfig.TestnetLedgerPublicRPCURL, "override solana rpc endpoint url")
	probeInterval        = flag.Int("probe-interval", 30, "latency probe interval in seconds")
	cacheUpdateInterval  = flag.Int("cache-update-interval", 30, "latency cache update interval in seconds")
	enableVerboseLogging = flag.Bool("v", false, "enables verbose logging")
	metricsEnable        = flag.Bool("metrics-enable", false, "Enable prometheus metrics")
	metricsAddr          = flag.String("metrics-addr", "localhost:0", "Address to listen on for prometheus metrics")

	// set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	flag.Parse()

	opts := &slog.HandlerOptions{}
	if *enableVerboseLogging {
		opts = &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, opts))
	slog.SetDefault(log)

	if *versionFlag {
		fmt.Printf("build: %s\n", commit)
		fmt.Printf("version: %s\n", version)
		fmt.Printf("date: %s\n", date)
		os.Exit(0)
	}

	if *metricsEnable {
		buildInfo := promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "doublezero_build_info",
				Help: "Build information of the client",
			},
			[]string{"version", "commit", "date"},
		)
		buildInfo.WithLabelValues(version, commit, date).Set(1)

		go func() {
			listener, err := net.Listen("tcp", *metricsAddr)
			if err != nil {
				log.Error("Failed to start prometheus metrics listener", "error", err)
				os.Exit(1)
			}
			http.Handle("/metrics", promhttp.Handler())

			log.Info("prometheus metrics server started", "address", listener.Addr().String())
			if err := http.Serve(listener, nil); err != nil {
				log.Error("Failed to start prometheus metrics server", "error", err)
			}
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// If program ID or RPC endpoint flags are set, but the config file is not present, then
	// initialize it with the flags. Otherwise, ignore the flags and log a warning.
	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		if *programId != "" && *rpcEndpoint == "" {
			log.Error("If program ID is set, RPC endpoint must also be set")
			os.Exit(1)
		}
		if *rpcEndpoint != "" && *programId == "" {
			log.Error("If RPC endpoint is set, program ID must also be set")
			os.Exit(1)
		}
		if *programId != "" && *rpcEndpoint != "" {
			log.Info("Initializing config file with flags", "program-id", *programId, "rpc-endpoint", *rpcEndpoint)
			cfg := config.New(*configPath)
			_, err := cfg.Update(*rpcEndpoint, solana.MustPublicKeyFromBase58(*programId))
			if err != nil {
				log.Error("Failed to save config", "error", err)
				os.Exit(1)
			}
		}
	} else if *programId != "" || *rpcEndpoint != "" {
		log.Warn("Ignoring program-id and solana-rpc-endpoint flags, config file is used instead")
	}

	if err := runtime.Run(ctx, log, *sockFile, *enableLatencyProbing, *configPath, *probeInterval, *cacheUpdateInterval); err != nil {
		log.Error("runtime error", "error", err)
		os.Exit(1)
	}
}
