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

	"github.com/gagliardetto/solana-go"
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
	routeLivenessTxMin       = flag.Duration("route-liveness-tx-min", defaultRouteLivenessTxMin, "route liveness tx min")
	routeLivenessRxMin       = flag.Duration("route-liveness-rx-min", defaultRouteLivenessRxMin, "route liveness rx min")
	routeLivenessDetectMult  = flag.Uint("route-liveness-detect-mult", defaultRouteLivenessDetectMult, "route liveness detect mult")
	routeLivenessMinTxFloor  = flag.Duration("route-liveness-min-tx-floor", defaultRouteLivenessMinTxFloor, "route liveness min tx floor")
	routeLivenessMaxTxCeil   = flag.Duration("route-liveness-max-tx-ceil", defaultRouteLivenessMaxTxCeil, "route liveness max tx ceil")
	routeLivenessPeerMetrics = flag.Bool("route-liveness-peer-metrics", false, "enables per peer metrics for route liveness (high cardinality)")
	routeLivenessDebug       = flag.Bool("route-liveness-debug", false, "enables debug logging for route liveness")

	// TODO(snormore): These flags are temporary for initial rollout testing.
	// They will be superceded by a single `route-liveness-enable` flag, where false means
	// passive-mode and true means active-mode.
	routeLivenessEnablePassive = flag.Bool("route-liveness-enable-passive", true, "enables route liveness in passive mode")
	routeLivenessEnableActive  = flag.Bool("route-liveness-enable-active", false, "enables route liveness in active mode (experimental)")

	// set by LDFLAGS
	version = "0.0.0-dev"
	commit  = "none"
	date    = "unknown"
)

const (
	defaultRouteLivenessTxMin      = 300 * time.Millisecond
	defaultRouteLivenessRxMin      = 300 * time.Millisecond
	defaultRouteLivenessDetectMult = 3
	defaultRouteLivenessMinTxFloor = 50 * time.Millisecond
	defaultRouteLivenessMaxTxCeil  = 1 * time.Second

	defaultRouteLivenessBindIP = "0.0.0.0"
)

func main() {
	flag.Parse()

	level := slog.LevelInfo
	if *enableVerboseLogging {
		level = slog.LevelDebug
	}
	logger := newLogger(level)
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

	var networkConfig *config.NetworkConfig
	if *env != "" {
		var err error
		networkConfig, err = config.NetworkConfigForEnv(*env)
		if err != nil {
			slog.Error("failed to get network config", "error", err)
			os.Exit(1)
		}
	}

	if networkConfig == nil {
		if *programId == "" {
			slog.Error("program-id is required")
			os.Exit(1)
		}
		if *rpcEndpoint == "" {
			slog.Error("rpc-endpoint is required")
			os.Exit(1)
		}
	} else {
		if *programId != "" {
			networkConfig.ServiceabilityProgramID = solana.MustPublicKeyFromBase58(*programId)
		}
		if *rpcEndpoint != "" {
			networkConfig.LedgerPublicRPCURL = *rpcEndpoint
		}
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

	// If either passive or active mode is enabled, create a manager config.
	// If neither is enabled, completely disable the liveness subsystem.
	// TODO(snormore): The scenario where the liveness subsystem is completely disabled is
	// temporary for initial rollout testing.
	var lmc *liveness.ManagerConfig
	if *routeLivenessEnablePassive || *routeLivenessEnableActive {
		log := logger
		if *routeLivenessDebug {
			log = newLogger(slog.LevelDebug)
		}
		lmc = &liveness.ManagerConfig{
			Logger:        log,
			BindIP:        defaultRouteLivenessBindIP,
			Port:          liveness.DefaultLivenessPort,
			ClientVersion: version,

			// If active mode is enabled, set passive mode to false.
			// The manager only knows about passive mode, with the negation of it being active mode.
			PassiveMode: !*routeLivenessEnableActive,

			TxMin:      *routeLivenessTxMin,
			RxMin:      *routeLivenessRxMin,
			DetectMult: uint8(*routeLivenessDetectMult),
			MinTxFloor: *routeLivenessMinTxFloor,
			MaxTxCeil:  *routeLivenessMaxTxCeil,

			EnablePeerMetrics: *routeLivenessPeerMetrics,

			// Default to treating peers that advertise passive mode as passive. That is, we will
			// install their routes immediately and never uninstall them on down events.
			HonorPeerAdvertisedPassive: true,
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runtime.Run(ctx, *sockFile, *routeConfigPath, *enableLatencyProbing, *enableLatencyMetrics, networkConfig, *probeInterval, *cacheUpdateInterval, lmc); err != nil {
		slog.Error("runtime error", "error", err)
		os.Exit(1)
	}
}

func newLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
}
