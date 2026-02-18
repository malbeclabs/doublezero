package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jonboulle/clockwork"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/config"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/server"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/rpc"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	flag "github.com/spf13/pflag"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	defaultPort                   = "8080"
	defaultMetricsAddr            = ":2112"
	defaultMetricsShutdownTimeout = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if cfg.ShowVersion {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		return nil
	}

	log := newLogger(cfg.Verbose)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var metricsErrCh <-chan error
	if cfg.MetricsAddr != "" {
		server.BuildInfo.WithLabelValues(version, commit, date).Set(1)
		metricsErrCh = startMetricsServer(ctx, log, cfg.MetricsAddr, defaultMetricsShutdownTimeout)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}
	s3Client := s3.NewFromConfig(awsCfg)
	presignClient := s3.NewPresignClient(s3Client)

	// DoubleZero configuration.
	dzNetworkConfig, err := config.NetworkConfigForEnv(cfg.Env)
	if err != nil {
		return fmt.Errorf("failed to get doublezero network config: %w", err)
	}
	ledgerRPC := rpc.NewWithRetries(dzNetworkConfig.LedgerPublicRPCURL, nil)
	defer ledgerRPC.Close()
	serviceabilityRPC := serviceability.New(ledgerRPC, dzNetworkConfig.ServiceabilityProgramID)

	srv, err := server.New(
		log,
		server.Config{
			Clock:             clockwork.NewRealClock(),
			Presign:           presignClient,
			BucketName:        cfg.S3Bucket,
			BucketPathPrefix:  cfg.S3Prefix,
			ServiceabilityRPC: serviceabilityRPC,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	defer listener.Close()

	log.Info("listening on", "address", listener.Addr().String())
	errCh := srv.Start(ctx, cancel, listener)

	for {
		select {
		case err, ok := <-errCh:
			if !ok {
				log.Info("server stopped")
				return nil
			}
			if err != nil {
				return fmt.Errorf("server error: %w", err)
			}
		case err, ok := <-metricsErrCh:
			if ok && err != nil {
				return fmt.Errorf("metrics server error: %w", err)
			}
			metricsErrCh = nil
		case <-ctx.Done():
			return nil
		}
	}
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

func startMetricsServer(ctx context.Context, log *slog.Logger, addr string, shutdownTimeout time.Duration) <-chan error {
	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)

		listener, err := net.Listen("tcp", addr)
		if err != nil {
			errCh <- err
			return
		}
		defer listener.Close()

		log.Info("prometheus metrics server listening", "address", listener.Addr().String())

		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		httpSrv := &http.Server{Handler: mux}

		go func() {
			<-ctx.Done()
			sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			_ = httpSrv.Shutdown(sctx)
		}()

		err = httpSrv.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		if err != nil {
			errCh <- err
		}
	}()

	return errCh
}

type Config struct {
	ShowVersion bool
	Verbose     bool
	MetricsAddr string
	Env         string

	Port     string
	S3Bucket string
	S3Prefix string
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func loadConfig() (Config, error) {
	var cfg Config

	flag.BoolVar(&cfg.ShowVersion, "version", false, "show version and exit")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "verbose mode - show debug logs")

	flag.StringVar(&cfg.MetricsAddr, "metrics-addr", getenv("METRICS_ADDR", defaultMetricsAddr), "address to listen on for prometheus metrics (env: METRICS_ADDR)")
	flag.StringVar(&cfg.Env, "env", getenv("DZ_ENV", config.EnvDevnet), "doublezero environment to use (env: DZ_ENV)")
	flag.StringVar(&cfg.Port, "port", getenv("PORT", defaultPort), "http listen port (env: PORT)")

	flag.StringVar(&cfg.S3Bucket, "s3-bucket", getenv("S3_BUCKET", ""), "s3 bucket name (env: S3_BUCKET)")
	flag.StringVar(&cfg.S3Prefix, "s3-prefix", getenv("S3_PREFIX", ""), "s3 prefix/path (env: S3_PREFIX)")

	flag.Parse()

	if cfg.ShowVersion {
		return cfg, nil
	}

	if cfg.S3Bucket == "" {
		return Config{}, fmt.Errorf("s3 bucket is empty (set S3_BUCKET or --s3-bucket)")
	}

	return cfg, nil
}
