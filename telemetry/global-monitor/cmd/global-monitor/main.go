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

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	influxdb2api "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/jonboulle/clockwork"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/dz"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/geoip"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/gm"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/metrics"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/netlink"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/netutil"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/sol"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/metrodb"
	"github.com/oschwald/geoip2-golang"
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
	defaultGeoipCityDBPath      = "/usr/share/GeoIP/GeoLite2-City.mmdb"
	defaultGeoipASNDBPath       = "/usr/share/GeoIP/GeoLite2-ASN.mmdb"
	defaultMetricsAddr          = ":8080"
	defaultProbeInterval        = 60 * time.Second
	defaultProbeTimeout         = 8 * time.Second
	defaultKeepAlivePeriod      = 1 * time.Second
	defaultMaxIdleTimeout       = 5 * time.Second
	defaultHandshakeIdleTimeout = 2 * time.Second
	defaultMaxConcurrency       = 32
	defaultRedialPeriod         = 10 * time.Minute
)

var (
	sourceMetroNames = map[string]string{
		"ams": "Amsterdam",
		"atl": "Atlanta",
		"blr": "Bangalore",
		"fra": "Frankfurt",
		"lon": "London",
		"nyc": "New York",
		"sfo": "San Francisco",
		"sgp": "Singapore",
		"syd": "Sydney",
		"tor": "Toronto",
	}
)

func main() {
	showVersionFlag := flag.Bool("version", false, "show version and exit")
	verboseFlag := flag.Bool("verbose", false, "verbose mode - show debug logs")
	dzEnvFlag := flag.String("dz-env", config.EnvMainnetBeta, "doublezero environment to use")
	solanaEnvFlag := flag.String("solana-env", config.SolanaEnvMainnetBeta, "solana environment to use")

	// Probe configuration.
	probeIntervalFlag := flag.Duration("probe-interval", defaultProbeInterval, "interval between probes per target")
	probeTimeoutFlag := flag.Duration("probe-timeout", defaultProbeTimeout, "timeout for each probe")
	keepAlivePeriodFlag := flag.Duration("keep-alive-period", defaultKeepAlivePeriod, "keep alive period for the probe")
	maxIdleTimeoutFlag := flag.Duration("max-idle-timeout", defaultMaxIdleTimeout, "max idle timeout for the probe")
	handshakeIdleTimeoutFlag := flag.Duration("handshake-idle-timeout", defaultHandshakeIdleTimeout, "handshake idle timeout for the probe")
	maxConcurrencyFlag := flag.Int("max-concurrency", defaultMaxConcurrency, "maximum number of concurrent probes")
	redialPeriodFlag := flag.Duration("redial-period", defaultRedialPeriod, "period to redial the connection")

	// Source configuration.
	publicIfaceFlag := flag.String("public-iface", "", "public internet interface to monitor solana (default: auto-detected)")
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

	log := newLogger(*verboseFlag)

	dzNetworkConfig, err := config.NetworkConfigForEnv(*dzEnvFlag)
	if err != nil {
		log.Error("failed to get doublezero network config", "error", err)
		os.Exit(1)
	}

	solanaNetworkConfig, err := config.SolanaNetworkConfigForEnv(*solanaEnvFlag)
	if err != nil {
		log.Error("failed to get solana network config", "error", err)
		os.Exit(1)
	}

	// Source configuration.
	sourceHost, err := os.Hostname()
	if err != nil {
		log.Error("failed to get hostname", "error", err)
		os.Exit(1)
	}
	if *sourceMetroFlag == "" {
		log.Error("source-metro is required")
		os.Exit(1)
	}
	sourceMetroName := sourceMetroNames[*sourceMetroFlag]
	if sourceMetroName == "" {
		log.Error("missing source metro name mapping", "sourceMetro", *sourceMetroFlag)
		os.Exit(1)
	}
	publicIface := *publicIfaceFlag
	if publicIface == "" {
		defaultInterface, err := netutil.DefaultInterface()
		if err != nil {
			log.Error("failed to get default interface", "error", err)
			os.Exit(1)
		}
		publicIface = defaultInterface.Name
		log.Info("using default interface as public internet interface", "interface", publicIface)
	}
	_, publicIP, err := netutil.ResolveInterface(publicIface)
	if err != nil {
		log.Error("public interface resolution failed", "interface", publicIface, "error", err)
		os.Exit(1)
	}
	dzIface := *dzIfaceFlag
	var dzIP string
	if dzIface != "" {
		_, ip, err := netutil.ResolveInterface(dzIface)
		if err != nil {
			log.Error("doublezero interface resolution failed", "interface", dzIface, "error", err)
			os.Exit(1)
		}
		dzIP = ip
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

	solanaRPC := solanarpc.New(solanaNetworkConfig.RPCURL)

	// GeoIP configuration.
	cityDB, err := geoip2.Open(*geoipCityDBPathFlag)
	if err != nil {
		log.Error("failed to open city db", "error", err)
		os.Exit(1)
	}
	defer cityDB.Close()
	asnDB, err := geoip2.Open(*geoipASNDBPathFlag)
	if err != nil {
		log.Error("failed to open asn db", "error", err)
		os.Exit(1)
	}
	defer asnDB.Close()
	metroDB, err := metrodb.New()
	if err != nil {
		log.Error("failed to create metro db", "error", err)
		os.Exit(1)
	}

	// DoubleZero configuration.
	ledgerRPC := solanarpc.New(dzNetworkConfig.LedgerPublicRPCURL)
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
	}

	nlr := netlink.NewNetlinker()

	geoIP, err := geoip.NewResolver(log, cityDB, asnDB, metroDB)
	if err != nil {
		log.Error("failed to create geoip resolver", "error", err)
		return
	}
	validatorsView, err := sol.NewValidatorsView(log, solanaRPC, geoIP)
	if err != nil {
		log.Error("failed to create validators view", "error", err)
		return
	}
	serviceabilityView, err := dz.NewServiceabilityView(log, serviceabilityRPC)
	if err != nil {
		log.Error("failed to create serviceability view", "error", err)
		return
	}

	publicProber, err := gm.NewTPUQUICProber(log)
	if err != nil {
		log.Error("failed to create public prober", "error", err)
		return
	}

	dzProber, err := gm.NewTPUQUICProber(log)
	if err != nil {
		log.Error("failed to create dz prober", "error", err)
		return
	}

	runner, err := gm.NewRunner(log, &gm.RunnerConfig{
		Clock:               clock,
		Validators:          validatorsView,
		Serviceability:      serviceabilityView,
		PublicTPUQUICProber: publicProber,
		DZTPUQUICProber:     dzProber,
		Netlinker:           nlr,
		DZNetworkEnv:        *dzEnvFlag,

		// Source configuration.
		SourcePublicIface: publicIface,
		SourceDZIface:     dzIface,
		SourceMetro:       *sourceMetroFlag,
		SourceMetroName:   sourceMetroName,
		SourceHost:        sourceHost,
		SourcePublicIP:    publicIP,
		SourceDZIP:        dzIP,

		// InfluxDB configuration.
		InfluxAPI: influxAPI,

		// Probe configuration.
		ProbeInterval:        *probeIntervalFlag,
		ProbeTimeout:         *probeTimeoutFlag,
		KeepAlivePeriod:      *keepAlivePeriodFlag,
		MaxIdleTimeout:       *maxIdleTimeoutFlag,
		HandshakeIdleTimeout: *handshakeIdleTimeoutFlag,
		MaxConcurrency:       *maxConcurrencyFlag,
		RedialPeriod:         *redialPeriodFlag,
	})
	if err != nil {
		log.Error("failed to create runner", "error", err)
		return
	}
	runner.Start(ctx)

	<-ctx.Done()
	log.Info("context done, stopping")
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
