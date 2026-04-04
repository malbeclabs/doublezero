// filename: main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	influxdb2api "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/jonboulle/clockwork"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/config"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/dz"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/gm"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/metrics"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/netlink"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/netutil"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/sol"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/metrodb"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/rpc"
	"github.com/oschwald/geoip2-golang"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	flag "github.com/spf13/pflag"

	_ "net/http/pprof"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	defaultGeoipCityDBPath      = "/usr/share/GeoIP/GeoLite2-City.mmdb"
	defaultGeoipASNDBPath       = "/usr/share/GeoIP/GeoLite2-ASN.mmdb"
	defaultMetricsAddr          = ":8080"
	defaultProbeInterval        = 60 * time.Second
	defaultProbeTimeout         = 8 * time.Second
	defaultKeepAlivePeriod      = 1 * time.Second
	defaultMaxIdleTimeout       = 5 * time.Second
	defaultHandshakeIdleTimeout = 2 * time.Second
	defaultMaxConcurrency       = 128
)

var (
	sourceMetroNames = map[string]string{
		"auh": "Abu Dhabi",
		"ams": "Amsterdam",
		"atl": "Atlanta",
		"blr": "Bangalore",
		"bom": "Mumbai",
		"cpt": "Cape Town",
		"fra": "Frankfurt",
		"hkg": "Hong Kong",
		"lon": "London",
		"nyc": "New York",
		"sao": "São Paulo",
		"sfo": "San Francisco",
		"sgp": "Singapore",
		"syd": "Sydney",
		"tpe": "Taipei",
		"tyo": "Tokyo",
		"tor": "Toronto",
	}
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	showVersionFlag := flag.Bool("version", false, "show version and exit")
	verboseFlag := flag.Bool("verbose", false, "verbose mode - show debug logs")
	verboseFailuresFlag := flag.Bool("verbose-failures", false, "verbose mode - show failure logs")
	verboseSuccessesFlag := flag.Bool("verbose-successes", false, "verbose mode - show success logs")
	enablePprofFlag := flag.Bool("enable-pprof", false, "enable pprof server")
	dzEnvFlag := flag.String("dz-env", config.EnvMainnetBeta, "doublezero environment to use")
	solanaEnvFlag := flag.String("solana-env", config.SolanaEnvMainnetBeta, "solana environment to use")

	// Probe configuration.
	probeIntervalFlag := flag.Duration("probe-interval", defaultProbeInterval, "interval between probes per target")
	probeTimeoutFlag := flag.Duration("probe-timeout", defaultProbeTimeout, "timeout for each probe")
	keepAlivePeriodFlag := flag.Duration("keep-alive-period", defaultKeepAlivePeriod, "keep alive period for the probe")
	maxIdleTimeoutFlag := flag.Duration("max-idle-timeout", defaultMaxIdleTimeout, "max idle timeout for the probe")
	handshakeIdleTimeoutFlag := flag.Duration("handshake-idle-timeout", defaultHandshakeIdleTimeout, "handshake idle timeout for the probe")
	maxConcurrencyFlag := flag.Int("max-concurrency", defaultMaxConcurrency, "maximum number of concurrent probes")

	// Source configuration.
	publicIfaceFlag := flag.String("public-iface", "", "public internet interface to monitor solana (default: auto-detected)")
	publicIPFlag := flag.String("public-ip", "", "public internet ip to monitor solana (default: auto-detected)")
	dzIfaceFlag := flag.String("dz-iface", "", "doublezero interface to monitor solana (default: auto-detected)")
	sourceMetroFlag := flag.String("source-metro", "", "source metro to monitor solana (required)")

	// GeoIP configuration.
	geoipCityDBPathFlag := flag.String("geoip-city-db-path", defaultGeoipCityDBPath, "path to the geoip city database")
	geoipASNDBPathFlag := flag.String("geoip-asn-db-path", defaultGeoipASNDBPath, "path to the geoip asn database")

	// Prometheus metrics configuration.
	metricsAddrFlag := flag.String("metrics-addr", defaultMetricsAddr, "Address to listen on for prometheus metrics")

	flag.Parse()

	if *showVersionFlag {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		os.Exit(0)
	}

	verbose := *verboseFlag || *verboseFailuresFlag || *verboseSuccessesFlag
	log := newLogger(verbose)

	// Start pprof server
	if *enablePprofFlag {
		go func() {
			log.Info("starting pprof server", "address", "localhost:6060")
			err := http.ListenAndServe("localhost:6060", nil)
			if err != nil {
				log.Error("failed to start pprof server", "error", err)
			}
		}()
	}

	dzNetworkConfig, err := config.NetworkConfigForEnv(*dzEnvFlag)
	if err != nil {
		log.Error("failed to get doublezero network config", "error", err)
		return err
	}

	solanaNetworkConfig, err := config.SolanaNetworkConfigForEnv(*solanaEnvFlag)
	if err != nil {
		log.Error("failed to get solana network config", "error", err)
		return err
	}

	publicIface := *publicIfaceFlag
	if publicIface == "" {
		defaultInterface, err := netutil.DefaultInterface()
		if err != nil {
			log.Error("failed to get default interface", "error", err)
			return err
		}
		publicIface = defaultInterface.Name
		log.Info("using default interface as public internet interface", "interface", *publicIfaceFlag)
	}

	var publicIP net.IP
	if *publicIPFlag != "" {
		publicIP = net.ParseIP(*publicIPFlag)
		if publicIP == nil {
			log.Error("failed to parse public ip", "ip", *publicIPFlag)
			return fmt.Errorf("failed to parse public ip: %s", *publicIPFlag)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Set up prometheus metrics server if enabled.
	if *metricsAddrFlag != "" {
		metrics.BuildInfo.WithLabelValues(version, commit, date).Set(1)
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

	clock := clockwork.NewRealClock()

	solanaRPC := rpc.NewWithRetries(solanaNetworkConfig.RPCURL, nil)
	defer solanaRPC.Close()

	// GeoIP configuration.
	cityDB, err := geoip2.Open(*geoipCityDBPathFlag)
	if err != nil {
		log.Error("failed to open city db", "error", err)
		return err
	}
	defer cityDB.Close()
	asnDB, err := geoip2.Open(*geoipASNDBPathFlag)
	if err != nil {
		log.Error("failed to open asn db", "error", err)
		return err
	}
	defer asnDB.Close()
	metroDB, err := metrodb.New()
	if err != nil {
		log.Error("failed to create metro db", "error", err)
		return err
	}

	// DoubleZero configuration.
	ledgerRPC := rpc.NewWithRetries(dzNetworkConfig.LedgerPublicRPCURL, nil)
	defer ledgerRPC.Close()
	serviceabilityRPC := serviceability.New(ledgerRPC, dzNetworkConfig.ServiceabilityProgramID)

	// InfluxDB configuration.
	influxUrl := os.Getenv("INFLUX_URL")
	influxToken := os.Getenv("INFLUX_TOKEN")
	influxOrg := os.Getenv("INFLUX_ORG")
	influxBucket := os.Getenv("INFLUX_BUCKET")
	influxEnabled := influxUrl != "" && influxToken != "" && influxOrg != "" && influxBucket != ""
	var influxAPI influxdb2api.WriteAPI
	if influxEnabled {
		client := influxdb2.NewClient(influxUrl, influxToken)
		defer client.Close()
		influxAPI = client.WriteAPI(influxOrg, influxBucket)
		defer influxAPI.Flush()
	}

	nlr := netlink.NewNetlinker()

	geoIP, err := geoip.NewResolver(log, cityDB, asnDB, metroDB)
	if err != nil {
		log.Error("failed to create geoip resolver", "error", err)
		return err
	}
	solanaView, err := sol.NewSolanaView(log, solanaRPC, geoIP)
	if err != nil {
		log.Error("failed to create solana view", "error", err)
		return err
	}
	serviceabilityView, err := dz.NewServiceabilityView(log, serviceabilityRPC)
	if err != nil {
		log.Error("failed to create serviceability view", "error", err)
		return err
	}

	runner, err := gm.NewRunner(log, &gm.RunnerConfig{
		Clock:          clock,
		Solana:         solanaView,
		Serviceability: serviceabilityView,
		Netlinker:      nlr,
		DZNetworkEnv:   *dzEnvFlag,

		// Verbosity configuration.
		VerboseFailures:  *verboseFailuresFlag,
		VerboseSuccesses: *verboseSuccessesFlag,

		// Source configuration.
		Source: &gm.SourceConfig{
			Serviceability: serviceabilityView,
			DZNetworkEnv:   *dzEnvFlag,
			PublicIface:    publicIface,
			PublicIP:       publicIP,
			DZIface:        *dzIfaceFlag,
			Metro:          *sourceMetroFlag,
			MetroNames:     sourceMetroNames,
		},

		// InfluxDB configuration.
		InfluxAPI: influxAPI,

		// GeoIP configuration.
		GeoIP: geoIP,

		// Probe configuration.
		ProbeInterval:        *probeIntervalFlag,
		ProbeTimeout:         *probeTimeoutFlag,
		KeepAlivePeriod:      *keepAlivePeriodFlag,
		MaxIdleTimeout:       *maxIdleTimeoutFlag,
		HandshakeIdleTimeout: *handshakeIdleTimeoutFlag,
		MaxConcurrency:       *maxConcurrencyFlag,
	})
	if err != nil {
		log.Error("failed to create runner", "error", err)
		return err
	}

	errCh := runner.Start(ctx)
	select {
	case err := <-errCh:
		log.Error("runner: error", "error", err)
		return err
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
