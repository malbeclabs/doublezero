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

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/server"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("failed to run: %v", err)
	}
}

func run() error {
	showVersionFlag := flag.Bool("version", false, "show version and exit")
	verboseFlag := flag.Bool("verbose", false, "verbose mode - show debug logs")
	metricsAddrFlag := flag.String("metrics-addr", ":2112", "Address to listen on for prometheus metrics")
	dzEnvFlag := flag.String("env", config.EnvDevnet, "doublezero environment to use")

	flag.Parse()

	if *showVersionFlag {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		os.Exit(0)
	}

	verbose := *verboseFlag
	log := newLogger(verbose)

	// Set up prometheus metrics server if enabled.
	if *metricsAddrFlag != "" {
		server.BuildInfo.WithLabelValues(version, commit, date).Set(1)
		go func() {
			listener, err := net.Listen("tcp", *metricsAddrFlag)
			if err != nil {
				log.Error("Failed to start prometheus metrics server listener", "error", err)
				os.Exit(1)
			}
			log.Info("Prometheus metrics server listening", "address", listener.Addr().String())
			http.Handle("/metrics", promhttp.Handler())
			if err := http.Serve(listener, nil); err != nil {
				log.Error("Failed to start prometheus metrics server", "error", err)
				os.Exit(1)
			}
		}()
	}

	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		return fmt.Errorf("S3_BUCKET is required")
	}
	prefix := os.Getenv("S3_PREFIX")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}
	s3Client := s3.NewFromConfig(cfg)
	presignClient := s3.NewPresignClient(s3Client)

	// DoubleZero configuration.
	dzNetworkConfig, err := config.NetworkConfigForEnv(*dzEnvFlag)
	if err != nil {
		return fmt.Errorf("failed to get doublezero network config: %w", err)
	}
	ledgerRPC := solanarpc.New(dzNetworkConfig.LedgerPublicRPCURL)
	// TODO(snormore): Add retry logic to the solana RPC client.
	defer ledgerRPC.Close()
	serviceabilityRPC := serviceability.New(ledgerRPC, dzNetworkConfig.ServiceabilityProgramID)
	// TODO(snormore): Pass this to the server and use it for device authentication.
	_ = serviceabilityRPC

	srv, err := server.New(
		log,
		presignClient,
		bucket,
		prefix,
		func(ctx context.Context, pubkey string) bool {
			return true
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	defer listener.Close()

	log.Info("listening on", "address", listener.Addr().String())
	errCh := srv.Start(ctx, cancel, listener)
	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		log.Info("context done, stopping")
	}
	return nil
}

func newLogger(verbose bool) *slog.Logger {
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	return slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level: logLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				t := a.Value.Time().UTC()
				a.Value = slog.StringValue(formatRFC3339Millis(t))
			}
			if s, ok := a.Value.Any().(string); ok && s == "" {
				return slog.Attr{}
			}
			return a
		},
	}))
}

func formatRFC3339Millis(t time.Time) string {
	t = t.UTC()
	base := t.Format("2006-01-02T15:04:05")
	ms := t.Nanosecond() / 1_000_000
	return fmt.Sprintf("%s.%03dZ", base, ms)
}
