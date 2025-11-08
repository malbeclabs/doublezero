package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/liveness"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/runtime"
	"github.com/malbeclabs/doublezero/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	sockFile             = flag.String("sock-file", "/var/run/doublezerod/doublezerod.sock", "path to doublezerod domain socket")
	enableLatencyProbing = flag.Bool("latency-probing", true, "enable latency probing to doublezero nodes")
	versionFlag          = flag.Bool("version", false, "build version")
	env                  = flag.String("env", config.EnvTestnet, "environment to use")
	programId            = flag.String("program-id", "", "override smartcontract program id to monitor")
	rpcEndpoint          = flag.String("solana-rpc-endpoint", "", "override solana rpc endpoint url")
	probeInterval        = flag.Int("probe-interval", 30, "latency probe interval in seconds")
	cacheUpdateInterval  = flag.Int("cache-update-interval", 30, "latency cache update interval in seconds")
	enableVerboseLogging = flag.Bool("v", false, "enables verbose logging")
	enableLatencyMetrics = flag.Bool("enable-latency-metrics", false, "enables latency metrics")
	metricsEnable        = flag.Bool("metrics-enable", false, "Enable prometheus metrics")
	metricsAddr          = flag.String("metrics-addr", "localhost:0", "Address to listen on for prometheus metrics")
	routeConfigPath      = flag.String("route-config", "/var/lib/doublezerod/route-config.json", "path to route config file (unstable)")

	// Route liveness configuration flags.
	routeLivenessEnabled    = flag.Bool("route-liveness-enable", defaultRouteLivenessEnabled, "enables route liveness (unstable)")
	routeLivenessTxMin      = flag.Duration("route-liveness-tx-min", defaultRouteLivenessTxMin, "route liveness tx min")
	routeLivenessRxMin      = flag.Duration("route-liveness-rx-min", defaultRouteLivenessRxMin, "route liveness rx min")
	routeLivenessDetectMult = flag.Uint("route-liveness-detect-mult", defaultRouteLivenessDetectMult, "route liveness detect mult")
	routeLivenessMinTxFloor = flag.Duration("route-liveness-min-tx-floor", defaultRouteLivenessMinTxFloor, "route liveness min tx floor")
	routeLivenessMaxTxCeil  = flag.Duration("route-liveness-max-tx-ceil", defaultRouteLivenessMaxTxCeil, "route liveness max tx ceil")

	// set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	defaultRouteLivenessTxMin      = 300 * time.Millisecond
	defaultRouteLivenessRxMin      = 300 * time.Millisecond
	defaultRouteLivenessDetectMult = 3
	defaultRouteLivenessMinTxFloor = 50 * time.Millisecond
	defaultRouteLivenessMaxTxCeil  = 1 * time.Second

	// Default route liveness is disabled for initial phase of rollout. This starts the liveness
	// manager in passive-mode, where the protocol functions but the kernel routing table is not
	// managed. This is used to support incremental rollout.
	defaultRouteLivenessEnabled = false

	// The liveness port is not configurable since clients need to use the same one so they know
	// how to connect to each other.
	defaultRouteLivenessPort   = 44880
	defaultRouteLivenessBindIP = "0.0.0.0"
)

func main() {
	flag.Parse()

	opts := &slog.HandlerOptions{}
	if *enableVerboseLogging {
		opts = &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, opts))
	slog.SetDefault(logger)

	if *versionFlag {
		fmt.Printf("build: %s\n", commit)
		fmt.Printf("version: %s\n", version)
		fmt.Printf("date: %s\n", date)
		os.Exit(0)
	}

	if *env == "" && *programId == "" && *rpcEndpoint == "" {
		slog.Error("Either env or program-id and rpc-endpoint must be provided")
		os.Exit(1)
	}

	if *env != "" {
		networkConfig, err := config.NetworkConfigForEnv(*env)
		if err != nil {
			slog.Error("failed to get network config", "error", err)
			os.Exit(1)
		}
		if *programId == "" {
			*programId = networkConfig.ServiceabilityProgramID.String()
		}
		if *rpcEndpoint == "" {
			*rpcEndpoint = networkConfig.LedgerPublicRPCURL
		}
	}

	if *programId == "" {
		slog.Error("program-id is required")
		os.Exit(1)
	}
	if *rpcEndpoint == "" {
		slog.Error("rpc-endpoint is required")
		os.Exit(1)
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
				slog.Error("Failed to start prometheus metrics listener", "error", err)
				os.Exit(1)
			}
			http.Handle("/metrics", promhttp.Handler())

			slog.Info("prometheus metrics server started", "address", listener.Addr().String())
			if err := http.Serve(listener, nil); err != nil {
				log.Printf("Failed to start prometheus metrics server: %v", err)
			}
		}()
	}

	lmc := &liveness.ManagerConfig{
		Logger: slog.Default(),
		BindIP: defaultRouteLivenessBindIP,
		Port:   defaultRouteLivenessPort,

		PassiveMode: !*routeLivenessEnabled,

		TxMin:      *routeLivenessTxMin,
		RxMin:      *routeLivenessRxMin,
		DetectMult: uint8(*routeLivenessDetectMult),
		MinTxFloor: *routeLivenessMinTxFloor,
		MaxTxCeil:  *routeLivenessMaxTxCeil,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runtime.Run(ctx, *sockFile, *routeConfigPath, *enableLatencyProbing, *enableLatencyMetrics, *programId, *rpcEndpoint, *probeInterval, *cacheUpdateInterval, lmc); err != nil {
		slog.Error("runtime error", "error", err)
		os.Exit(1)
	}
}
