// Command allowlist-sync keeps the device address allowlists equal to the
// ledger plus the reviewed exception file: gnmic's address map ConfigMap and
// the Cloudflare IP list in front of gnmic and state-in.
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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/telemetry/allowlist-sync/internal/allowlist"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/rpc"
	"github.com/prometheus/client_golang/prometheus"
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
	defaultMetricsAddr     = ":2112"
	defaultInterval        = 60 * time.Second
	defaultPassTimeout     = 2 * time.Minute
	defaultCallerWindow    = 24 * time.Hour
	defaultGnmicMapKey     = "device-addresses.json"
	defaultCloudflareURL   = "https://api.cloudflare.com/client/v4"
	cloudflarePollInterval = 2 * time.Second
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

	metrics := allowlist.NewMetrics(prometheus.DefaultRegisterer)
	metrics.BuildInfo.WithLabelValues(version, commit, date).Set(1)
	metricsErrCh := startMetricsServer(ctx, log, cfg.MetricsAddr)

	networkConfig, err := config.NetworkConfigForEnv(cfg.Env)
	if err != nil {
		return fmt.Errorf("failed to get doublezero network config: %w", err)
	}
	rpcURL := cfg.LedgerRPCURL
	if rpcURL == "" {
		rpcURL = networkConfig.LedgerPublicRPCURL
	}
	ledgerRPC := rpc.NewWithRetries(rpcURL, nil)
	defer ledgerRPC.Close()

	callers, err := allowlist.NewClickHouseCallers(cfg.ClickHouseAddr, cfg.ClickHouseDB, cfg.ClickHouseUser, cfg.ClickHousePass, cfg.ClickHouseTLSDisabled)
	if err != nil {
		return err
	}
	defer callers.Close()

	configMap, err := allowlist.NewInClusterConfigMap(cfg.Namespace, cfg.GnmicMapConfigMap)
	if err != nil {
		return err
	}
	clock := clockwork.NewRealClock()
	targets := []allowlist.Target{
		&allowlist.GnmicMapTarget{API: configMap, Key: cfg.GnmicMapKey},
		&allowlist.CloudflareTarget{API: &allowlist.CloudflareIPList{
			Client:       &http.Client{Timeout: 30 * time.Second},
			BaseURL:      cfg.CloudflareURL,
			Token:        cfg.CloudflareToken,
			AccountID:    cfg.CloudflareAccountID,
			ListID:       cfg.CloudflareListID,
			Clock:        clock,
			PollInterval: cloudflarePollInterval,
		}},
	}

	syncer, err := allowlist.New(allowlist.Config{
		Log:            log,
		Clock:          clock,
		Metrics:        metrics,
		Ledger:         serviceability.New(ledgerRPC, networkConfig.ServiceabilityProgramID),
		ExceptionsFile: cfg.ExceptionsFile,
		Callers:        callers,
		CallerWindow:   cfg.CallerWindow,
		Targets:        targets,
		Interval:       cfg.Interval,
		PassTimeout:    cfg.PassTimeout,
		DryRun:         cfg.DryRun,
		AllowShrinkTo:  cfg.AllowShrinkTo,
	})
	if err != nil {
		return err
	}
	log.Info("allowlist sync starting", "env", cfg.Env, "dry_run", cfg.DryRun, "interval", cfg.Interval,
		"configmap", cfg.GnmicMapConfigMap, "key", cfg.GnmicMapKey, "cloudflare_list", cfg.CloudflareListID,
		"allow_shrink_to", cfg.AllowShrinkTo)

	syncErrCh := make(chan error, 1)
	go func() { syncErrCh <- syncer.Run(ctx) }()

	select {
	case err := <-syncErrCh:
		return err
	case err, ok := <-metricsErrCh:
		if ok && err != nil {
			return fmt.Errorf("metrics server error: %w", err)
		}
		return nil
	case <-ctx.Done():
		return nil
	}
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(tint.NewTextHandler(os.Stdout, &tint.Options{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().UTC().Format("2006-01-02T15:04:05.000Z"))
			}
			return a
		},
	}))
}

func startMetricsServer(ctx context.Context, log *slog.Logger, addr string) <-chan error {
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
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			<-ctx.Done()
			sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = srv.Shutdown(sctx)
		}()
		err = srv.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
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

	LedgerRPCURL   string
	ExceptionsFile string
	Interval       time.Duration
	PassTimeout    time.Duration
	DryRun         bool
	AllowShrinkTo  string

	ClickHouseAddr        string
	ClickHouseDB          string
	ClickHouseUser        string
	ClickHousePass        string
	ClickHouseTLSDisabled bool
	CallerWindow          time.Duration

	Namespace         string
	GnmicMapConfigMap string
	GnmicMapKey       string

	CloudflareURL       string
	CloudflareAccountID string
	CloudflareListID    string
	CloudflareToken     string
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// envErrs collects environment values that do not parse. A typo such as
// DRY_RUN=yes must stop the sync, not fall back to writing.
var envErrs []error

func getenvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		envErrs = append(envErrs, fmt.Errorf("%s=%q: %w", key, v, err))
	}
	return b
}

func getenvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		envErrs = append(envErrs, fmt.Errorf("%s=%q: %w", key, v, err))
	}
	return d
}

func loadConfig() (Config, error) {
	var cfg Config

	flag.BoolVar(&cfg.ShowVersion, "version", false, "show version and exit")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "verbose mode - show debug logs")
	flag.StringVar(&cfg.MetricsAddr, "metrics-addr", getenv("METRICS_ADDR", defaultMetricsAddr), "address to listen on for prometheus metrics (env: METRICS_ADDR)")
	flag.StringVar(&cfg.Env, "env", getenv("DZ_ENV", ""), "doublezero environment: mainnet-beta, testnet or devnet (env: DZ_ENV)")

	flag.StringVar(&cfg.LedgerRPCURL, "ledger-rpc-url", getenv("LEDGER_RPC_URL", ""), "ledger RPC URL; defaults to the environment's public RPC (env: LEDGER_RPC_URL)")
	flag.StringVar(&cfg.ExceptionsFile, "exceptions-file", getenv("EXCEPTIONS_FILE", ""), "path to the reviewed exception file, mounted from a ConfigMap (env: EXCEPTIONS_FILE)")
	flag.DurationVar(&cfg.Interval, "interval", getenvDuration("INTERVAL", defaultInterval), "time between passes (env: INTERVAL)")
	flag.DurationVar(&cfg.PassTimeout, "pass-timeout", getenvDuration("PASS_TIMEOUT", defaultPassTimeout), "time limit for one pass, Cloudflare's bulk operation included (env: PASS_TIMEOUT)")
	flag.BoolVar(&cfg.DryRun, "dry-run", getenvBool("DRY_RUN", false), "compute and log each pass's changes, write nothing (env: DRY_RUN)")
	flag.StringVar(&cfg.AllowShrinkTo, "allow-shrink-to", getenv("ALLOW_SHRINK_TO", ""), "digest of one list to apply although a removal limit refuses it; a refused pass logs it (env: ALLOW_SHRINK_TO)")

	flag.StringVar(&cfg.ClickHouseAddr, "clickhouse-addr", getenv("CLICKHOUSE_ADDR", ""), "ClickHouse address (env: CLICKHOUSE_ADDR)")
	flag.StringVar(&cfg.ClickHouseDB, "clickhouse-db", getenv("CLICKHOUSE_DB", ""), "database holding controller_grpc_getconfig_success (env: CLICKHOUSE_DB)")
	flag.StringVar(&cfg.ClickHouseUser, "clickhouse-user", getenv("CLICKHOUSE_USER", ""), "read-only ClickHouse user (env: CLICKHOUSE_USER)")
	flag.BoolVar(&cfg.ClickHouseTLSDisabled, "clickhouse-tls-disabled", getenvBool("CLICKHOUSE_TLS_DISABLED", false), "connect to ClickHouse without TLS (env: CLICKHOUSE_TLS_DISABLED)")
	flag.DurationVar(&cfg.CallerWindow, "caller-window", getenvDuration("CALLER_WINDOW", defaultCallerWindow), "how far back the report reads caller addresses (env: CALLER_WINDOW)")

	flag.StringVar(&cfg.Namespace, "namespace", getenv("POD_NAMESPACE", ""), "namespace of gnmic's map ConfigMap; defaults to the pod's own (env: POD_NAMESPACE)")
	flag.StringVar(&cfg.GnmicMapConfigMap, "gnmic-map-configmap", getenv("GNMIC_MAP_CONFIGMAP", ""), "name of gnmic's map ConfigMap (env: GNMIC_MAP_CONFIGMAP)")
	flag.StringVar(&cfg.GnmicMapKey, "gnmic-map-key", getenv("GNMIC_MAP_KEY", defaultGnmicMapKey), "ConfigMap key gnmic reads the map from (env: GNMIC_MAP_KEY)")

	flag.StringVar(&cfg.CloudflareURL, "cloudflare-api-url", getenv("CLOUDFLARE_API_URL", defaultCloudflareURL), "Cloudflare API base URL (env: CLOUDFLARE_API_URL)")
	flag.StringVar(&cfg.CloudflareAccountID, "cloudflare-account-id", getenv("CLOUDFLARE_ACCOUNT_ID", ""), "Cloudflare account that holds the IP list (env: CLOUDFLARE_ACCOUNT_ID)")
	flag.StringVar(&cfg.CloudflareListID, "cloudflare-list-id", getenv("CLOUDFLARE_LIST_ID", ""), "Cloudflare IP list the sync owns (env: CLOUDFLARE_LIST_ID)")

	flag.Parse()
	if cfg.ShowVersion {
		return cfg, nil
	}
	if err := errors.Join(envErrs...); err != nil {
		return Config{}, err
	}

	// Secrets come from the environment only, so they never show in a
	// process listing.
	cfg.ClickHousePass = os.Getenv("CLICKHOUSE_PASS")
	cfg.CloudflareToken = os.Getenv("CLOUDFLARE_API_TOKEN")

	required := []struct{ name, value string }{
		{"--env (DZ_ENV)", cfg.Env},
		{"--exceptions-file (EXCEPTIONS_FILE)", cfg.ExceptionsFile},
		{"--clickhouse-addr (CLICKHOUSE_ADDR)", cfg.ClickHouseAddr},
		{"--clickhouse-db (CLICKHOUSE_DB)", cfg.ClickHouseDB},
		{"--clickhouse-user (CLICKHOUSE_USER)", cfg.ClickHouseUser},
		{"--gnmic-map-configmap (GNMIC_MAP_CONFIGMAP)", cfg.GnmicMapConfigMap},
		{"--cloudflare-account-id (CLOUDFLARE_ACCOUNT_ID)", cfg.CloudflareAccountID},
		{"--cloudflare-list-id (CLOUDFLARE_LIST_ID)", cfg.CloudflareListID},
		{"CLOUDFLARE_API_TOKEN", cfg.CloudflareToken},
	}
	for _, r := range required {
		if r.value == "" {
			return Config{}, fmt.Errorf("%s is required", r.name)
		}
	}
	// The token can edit every list in the account; never send it in clear.
	if !strings.HasPrefix(cfg.CloudflareURL, "https://") {
		return Config{}, fmt.Errorf("--cloudflare-api-url must be https, got %q", cfg.CloudflareURL)
	}
	return cfg, nil
}
