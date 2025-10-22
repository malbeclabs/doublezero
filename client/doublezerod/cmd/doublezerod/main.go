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

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/manager"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/probing"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/runtime"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/services"
	"github.com/malbeclabs/doublezero/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultRouteProbingInterval       = 1 * time.Second
	defaultRouteProbingMaxConcurrency = 128
	defaultRouteProbingProbeTimeout   = 1 * time.Second
	defaultRouteProbingUpThreshold    = 3
	defaultRouteProbingDownThreshold  = 3
)

var (
	sockFile                   = flag.String("sock-file", "/var/run/doublezerod/doublezerod.sock", "path to doublezerod domain socket")
	enableLatencyProbing       = flag.Bool("latency-probing", true, "enable latency probing to doublezero nodes")
	versionFlag                = flag.Bool("version", false, "build version")
	env                        = flag.String("env", config.EnvTestnet, "environment to use")
	programId                  = flag.String("program-id", "", "override smartcontract program id to monitor")
	rpcEndpoint                = flag.String("solana-rpc-endpoint", "", "override solana rpc endpoint url")
	probeInterval              = flag.Int("probe-interval", 30, "latency probe interval in seconds")
	cacheUpdateInterval        = flag.Int("cache-update-interval", 30, "latency cache update interval in seconds")
	enableVerboseLogging       = flag.Bool("v", false, "enables verbose logging")
	enableLatencyMetrics       = flag.Bool("enable-latency-metrics", false, "enables latency metrics")
	metricsEnable              = flag.Bool("metrics-enable", false, "Enable prometheus metrics")
	metricsAddr                = flag.String("metrics-addr", "localhost:0", "Address to listen on for prometheus metrics")
	routeProbingEnable         = flag.Bool("route-probing-enable", false, "enables route liveness probing")
	routeProbingInterval       = flag.Duration("route-probing-interval", defaultRouteProbingInterval, "route liveness probing interval as a duration (i.e. 5s, 10s, 30s)")
	routeProbingMaxConcurrency = flag.Uint("route-probing-max-concurrency", defaultRouteProbingMaxConcurrency, "route liveness probing max concurrency")
	routeProbingProbeTimeout   = flag.Duration("route-probing-probe-timeout", defaultRouteProbingProbeTimeout, "route liveness probing probe timeout as a duration (i.e. 1s, 3s, 5s)")
	routeProbingUpThreshold    = flag.Uint("route-probing-up-threshold", defaultRouteProbingUpThreshold, "route liveness probing up threshold")
	routeProbingDownThreshold  = flag.Uint("route-probing-down-threshold", defaultRouteProbingDownThreshold, "route liveness probing down threshold")

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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := manager.NewDb()
	if err != nil {
		slog.Error("error initializing db", "error", err)
		os.Exit(1)
	}

	nlr := routing.Netlink{}

	bgps, err := bgp.NewBgpServer(net.IPv4(1, 1, 1, 1))
	if err != nil {
		slog.Error("error creating bgp server", "error", err)
		os.Exit(1)
	}

	pim := pim.NewPIMServer()

	services := map[api.UserType]manager.Provisioner{
		api.UserTypeIBRL: services.NewIBRLService(bgps, nlr, db, func(iface string, src net.IP) (bgp.RouteManager, error) {
			if *routeProbingEnable {
				pm, err := probing.NewProbingManager(probing.Config{
					Logger:         logger,
					Context:        ctx,
					Netlink:        nlr,
					Interval:       *routeProbingInterval,
					MaxConcurrency: *routeProbingMaxConcurrency,
					ProbeTimeout:   *routeProbingProbeTimeout,
					InterfaceName:  iface,
					TunnelSrc:      src,
					UpThreshold:    *routeProbingUpThreshold,
					DownThreshold:  *routeProbingDownThreshold,
				})
				if err != nil {
					slog.Error("error creating probing router", "error", err)
					os.Exit(1)
				}
				return pm, nil
			} else {
				return manager.NewNetlinkerRouteManager(nlr), nil
			}
		}),
		api.UserTypeIBRLWithAllocatedIP: services.NewIBRLServiceWithAllocatedAddress(bgps, nlr, db, func(iface string, src net.IP) (bgp.RouteManager, error) {
			// Use non-probing route manager for allocated IP case
			return manager.NewNetlinkerRouteManager(nlr), nil
		}),
		api.UserTypeEdgeFiltering: services.NewEdgeFilteringService(bgps, nlr, db, func(iface string, src net.IP) (bgp.RouteManager, error) {
			// Use non-probing route manager for edge filtering
			return manager.NewNetlinkerRouteManager(nlr), nil
		}),
		api.UserTypeMulticast: services.NewMulticastService(bgps, nlr, db, pim, func(iface string, src net.IP) (bgp.RouteManager, error) {
			// Use non-probing route manager for multicast
			return manager.NewNetlinkerRouteManager(nlr), nil
		}),
	}

	nlm := manager.NewNetlinkManager(nlr, bgps, db, services)

	if err := runtime.Run(ctx, nlm, *sockFile, *enableLatencyProbing, *enableLatencyMetrics, *programId, *rpcEndpoint, *probeInterval, *cacheUpdateInterval); err != nil {
		slog.Error("runtime error", "error", err)
		os.Exit(1)
	}
}
