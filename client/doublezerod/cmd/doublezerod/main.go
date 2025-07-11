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

	solana "github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/runtime"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	sockFile             = flag.String("sock-file", "/var/run/doublezerod/doublezerod.sock", "path to doublezerod domain socket")
	enableLatencyProbing = flag.Bool("latency-probing", true, "enable latency probing to doublezero nodes")
	versionFlag          = flag.Bool("version", false, "build version")
	programId            = flag.String("program-id", serviceability.SERVICEABILITY_PROGRAM_ID_TESTNET, "override smartcontract program id to monitor")
	rpcEndpoint          = flag.String("solana-rpc-endpoint", dzsdk.DZ_LEDGER_RPC_URL, "override solana rpc endpoint url")
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, opts))
	slog.SetDefault(logger)

	if *versionFlag {
		fmt.Printf("build: %s\n", commit)
		fmt.Printf("version: %s\n", version)
		fmt.Printf("date: %s\n", date)
		os.Exit(0)
	}

	if *programId != "" {
		_, err := solana.PublicKeyFromBase58(*programId)
		if err != nil {
			slog.Error("malformed smartcontract program-id", "error", err)
			os.Exit(1)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runtime.Run(ctx, *sockFile, *enableLatencyProbing, *programId, *rpcEndpoint, *probeInterval, *cacheUpdateInterval); err != nil {
		slog.Error("runtime error", "error", err)
		os.Exit(1)
	}
}
