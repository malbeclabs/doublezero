// filename: main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/jonboulle/clockwork"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/dzmon"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/gmon"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/influx"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/pubmon"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/solmon"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/summary"
	flag "github.com/spf13/pflag"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	showVersion := flag.Bool("version", false, "show version and exit")
	verbose := flag.Bool("verbose", false, "verbose mode - show debug logs")

	dzEnv := flag.String("dz-env", config.EnvMainnetBeta, "doublezero environment to use")
	solanaEnv := flag.String("solana-env", config.SolanaEnvMainnetBeta, "solana environment to use")
	solanaRefreshInterval := flag.Duration("refresh-interval", 30*time.Second, "interval to refresh validators data from solana rpc")

	publicIface := flag.String("public-iface", "", "public internet interface to monitor solana")
	dzIface := flag.String("dz-iface", "", "doublezero interface to monitor solana")
	probeInterval := flag.Duration("probe-interval", 10*time.Second, "interval between probes per target")
	warmupPeriod := flag.Duration("warmup-period", 30*time.Second, "period to warmup before counting as a failure")

	// sourceIP := flag.String("source-ip", "", "source ip to monitor solana")
	sourceMetro := flag.String("source-metro", "", "source metro to monitor solana")

	sourceHost, _ := os.Hostname()

	flag.Parse()

	if *showVersion {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		os.Exit(0)
	}

	log := newLogger(*verbose)

	if *sourceMetro == "" {
		slog.Error("source-metro is required")
		os.Exit(1)
	}

	dzNetworkConfig, err := config.NetworkConfigForEnv(*dzEnv)
	if err != nil {
		log.Error("failed to get doublezero network config", "error", err)
		os.Exit(1)
	}

	solanaNetworkConfig, err := config.SolanaNetworkConfigForEnv(*solanaEnv)
	if err != nil {
		log.Error("failed to get solana network config", "error", err)
		os.Exit(1)
	}

	if *publicIface == "" {
		defaultInterface, err := DefaultInterface()
		if err != nil {
			log.Error("failed to get default interface", "error", err)
			os.Exit(1)
		}
		*publicIface = defaultInterface.Name
		log.Info("using default interface as public internet interface", "interface", *publicIface)
	}

	if err := RequireInterface(*publicIface); err != nil {
		log.Error("failed to find public internet interface", "interface", *publicIface, "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	solanaRPC := solanarpc.New(solanaNetworkConfig.RPCURL)

	// Shared Solana validators view (single source of truth).
	validatorsView, err := solmon.NewValidatorsView(log, solanaRPC, *solanaRefreshInterval)
	if err != nil {
		log.Error("failed to create validators view", "error", err)
		os.Exit(1)
	}
	validatorsView.Start(ctx, cancel)

	// Target source for public internet TPU QUIC probes.)
	pubSource, err := pubmon.NewValidatorTargetSource(&pubmon.ValidatorTargetSourceConfig{
		Logger:     log,
		Clock:      clockwork.NewRealClock(),
		Validators: validatorsView,

		Interface:            *publicIface,
		MaxIdleTimeout:       10 * time.Second,
		HandshakeIdleTimeout: 5 * time.Second,
		KeepAlivePeriod:      1 * time.Second,
		WarmupPeriod:         *warmupPeriod,

		WindowSlots:      60,
		WindowResolution: 10 * time.Second,
		HealthEWMAAlpha:  0.2,

		SyncInterval: 15 * time.Second,
	})
	if err != nil {
		log.Error("failed to create public internet target source", "error", err)
		os.Exit(1)
	}
	pubSource.Start(ctx, cancel)

	var combinedSource gmon.TargetSource

	if *dzIface != "" {
		if err := RequireInterface(*dzIface); err != nil {
			log.Error("failed to find doublezero interface", "interface", *dzIface, "error", err)
			os.Exit(1)
		}

		ledgerRPC := solanarpc.New(dzNetworkConfig.LedgerPublicRPCURL)
		serviceabilityRPC := serviceability.New(ledgerRPC, dzNetworkConfig.ServiceabilityProgramID)
		serviceabilityView, err := dzmon.NewServiceabilityView(&dzmon.ServiceabilityViewConfig{
			Logger:          log,
			RPC:             serviceabilityRPC,
			RefreshInterval: 60 * time.Second,
		})
		if err != nil {
			log.Error("failed to create serviceability view", "error", err)
			os.Exit(1)
		}
		serviceabilityView.Start(ctx, cancel)

		daemonClient := dzmon.NewDaemonClient()

		dzSource, err := dzmon.NewDoubleZeroTargetSource(&dzmon.DoubleZeroTargetSourceConfig{
			Logger:         log,
			Clock:          clockwork.NewRealClock(),
			Validators:     validatorsView,
			Serviceability: serviceabilityView,
			Daemon:         daemonClient,

			Interface:            *dzIface,
			MaxIdleTimeout:       10 * time.Second,
			HandshakeIdleTimeout: 5 * time.Second,
			KeepAlivePeriod:      1 * time.Second,
			WarmupPeriod:         *warmupPeriod,

			WindowSlots:      60,
			WindowResolution: 10 * time.Second,
			HealthEWMAAlpha:  0.2,

			SyncInterval: 15 * time.Second,
		})
		if err != nil {
			log.Error("failed to create doublezero target source", "error", err)
			os.Exit(1)
		}
		dzSource.Start(ctx, cancel)

		combinedSource = gmon.NewFanInTargetSource(pubSource, dzSource)
	} else {
		combinedSource = gmon.NewFanInTargetSource(pubSource)
	}

	// Scheduler for ValidatorTarget probes.
	scheduler, err := gmon.NewScheduler(gmon.SchedulerConfig{
		Logger:        log,
		Source:        combinedSource,
		ProbeInterval: *probeInterval,

		ResultsBuffer:  1024,
		MaxConcurrency: 64,
		ProbeTimeout:   2 * time.Second,
	})
	if err != nil {
		log.Error("failed to create scheduler", "error", err)
		os.Exit(1)
	}

	// Start scheduler.
	scheduler.Start(ctx, cancel)

	getConfiguredTargets := func() []gmon.Target {
		m := combinedSource.All()
		targets := make([]gmon.Target, 0, len(m))
		for _, target := range m {
			targets = append(targets, target)
		}
		return targets
	}

	results := scheduler.Results()

	influxUrl := os.Getenv("INFLUX_URL")
	influxToken := os.Getenv("INFLUX_TOKEN")
	influxOrg := os.Getenv("INFLUX_ORG")
	influxBucket := os.Getenv("INFLUX_BUCKET")
	influxEnabled := influxUrl != "" && influxToken != "" && influxOrg != "" && influxBucket != ""

	summaryCh := make(chan gmon.ProbeResult, 1024)
	influxCh := make(chan influx.Sample, 4096)

	go func() {
		defer close(summaryCh)
		if influxEnabled {
			defer close(influxCh)
		}

		for r := range results {
			// Summary path: already non-blocking, maybe add a log if you care.
			select {
			case summaryCh <- r:
			default:
				// optional:
				// log.Warn("summary channel full, dropping result", "target", r.TargetID().String())
			}

			if !influxEnabled {
				continue
			}

			// Influx path: make it non-blocking too.
			switch v := r.(type) {
			case solmon.ValidatorProbeResult:
				sample := solmon.NewInfluxSample(v, map[string]string{
					"source_iface": *publicIface,
					"source_host":  sourceHost,
					"source_metro": *sourceMetro,
				})
				select {
				case influxCh <- sample:
				default:
					log.Warn("influx channel full, dropping sample",
						"target", v.TargetID().String())
				}

			case dzmon.DoubleZeroProbeResult:
				sample := dzmon.NewInfluxSample(v, map[string]string{
					"source_iface": *dzIface,
					"source_host":  sourceHost,
					"source_metro": *sourceMetro,
				})
				select {
				case influxCh <- sample:
				default:
					log.Warn("influx channel full, dropping sample",
						"target", v.TargetID().String())
				}

			default:
				// unknown probe type; ignore or log
				log.Warn("unknown probe result type",
					"target", r.TargetID().String(),
					"type", fmt.Sprintf("%T", r))
			}
		}
	}()

	// Aggregate scheduler results into a periodic health summary log.
	summary.StartSummaryLogger(
		ctx,
		summary.SummaryConfig{
			Logger:                log,
			LogInterval:           15 * time.Second,
			AvailabilityThreshold: 0.95,
		},
		summaryCh,
		func() int { return len(combinedSource.All()) },
		getConfiguredTargets,
	)

	// Export results to InfluxDB.
	if influxUrl != "" && influxToken != "" && influxOrg != "" && influxBucket != "" {
		log.Info("exporting metrics to influxdb", "url", influxUrl, "org", influxOrg, "bucket", influxBucket)
		influxClient := influxdb2.NewClient(influxUrl, influxToken)
		// TODO(snormore): Switch to the non-blocking/async writer.
		influxWriteAPI := influxClient.WriteAPIBlocking(influxOrg, influxBucket)
		exporter := influx.NewExporter(influx.ExporterConfig{
			Logger:     log,
			WriteAPI:   influxWriteAPI,
			BatchSize:  500,
			FlushEvery: 3 * time.Second,
		})
		exporter.Start(ctx, influxCh, cancel)
	} else {
		log.Warn("influx not configured; metrics exporter disabled")
	}

	<-ctx.Done()
	log.Info("context done, stopping", "reason", ctx.Err())
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

func RequireInterface(name string) error {
	exists, err := InterfaceExists(name)
	if err != nil {
		return fmt.Errorf("failed to check if interface exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("interface not found: %s", name)
	}
	return nil
}

func InterfaceExists(name string) (bool, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false, err
	}
	for _, iface := range ifaces {
		if iface.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func DefaultInterface() (*net.Interface, error) {
	// Pick any routable remote address; no packets are sent.
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, _ := a.(*net.IPNet)
			if ipnet == nil {
				continue
			}
			if ipnet.IP.Equal(localAddr.IP) {
				return &iface, nil
			}
		}
	}

	return nil, fmt.Errorf("default interface not found")
}
