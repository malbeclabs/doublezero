package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/config"
	solmon "github.com/malbeclabs/doublezero/tools/global-monitor/internal/solana"
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
	solanaEnv := flag.String("solana-env", config.SolanaEnvMainnetBeta, "solana environment to use")
	solanaRefreshInterval := flag.Duration("refresh-interval", 30*time.Second, "interval to refresh validators data from solana rpc")
	statsInterval := flag.Duration("stats-interval", 5*time.Second, "interval to monitor solana")
	keepAlivePeriod := flag.Duration("keep-alive-period", 1*time.Second, "keep alive period to monitor solana")
	handshakeIdleTimeout := flag.Duration("handshake-idle-timeout", 5*time.Second, "handshake idle timeout to monitor solana")
	probeDuration := flag.Duration("probe-duration", 0, "duration to run probe")
	probeAvailabilityThreshold := flag.Float64("probe-availability-threshold", 0.95, "availability threshold to consider a validator unhealthy")
	publicInternetInterface := flag.String("public-internet-iface", "", "public internet interface to monitor solana")
	// doublezeroInterface := flag.String("doublezero-iface", "doublezero0", "double zero interface to monitor solana")
	randomValidators := flag.Int("random-validators", 0, "choose a number of random validators to monitor")

	flag.Parse()

	if *showVersion {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		os.Exit(0)
	}

	log := newLogger(*verbose)

	solanaNetworkConfig, err := config.SolanaNetworkConfigForEnv(*solanaEnv)
	if err != nil {
		log.Error("failed to get solana network config", "error", err)
		os.Exit(1)
	}

	if *publicInternetInterface == "" {
		defaultInterface, err := DefaultInterface()
		if err != nil {
			log.Error("failed to get default interface", "error", err)
			os.Exit(1)
		}
		*publicInternetInterface = defaultInterface.Name
		log.Info("using default interface as public internet interface", "interface", *publicInternetInterface)
	}

	err = RequireInterface(*publicInternetInterface)
	if err != nil {
		log.Error("failed find public internet interface", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	solanaRPC := solanarpc.New(solanaNetworkConfig.RPCURL)

	validatorsView, err := solmon.NewValidatorsView(log, solanaRPC, *solanaRefreshInterval)
	if err != nil {
		log.Error("failed to create validators view", "error", err)
		os.Exit(1)
	}
	validatorsView.Start(ctx, cancel)

	var getValidatorsFunc func(ctx context.Context) (map[solana.PublicKey]*solmon.ValidatorView, error)
	if *randomValidators > 0 {
		all := validatorsView.All()
		keys := make([]solana.PublicKey, 0, len(all))
		for pk := range all {
			keys = append(keys, pk)
		}
		rand.Shuffle(len(keys), func(i, j int) {
			keys[i], keys[j] = keys[j], keys[i]
		})
		keys = keys[:*randomValidators]
		log.Info("using random validators", "count", len(keys))
		randomValidators := make(map[solana.PublicKey]*solmon.ValidatorView)
		for _, pk := range keys {
			randomValidators[pk] = all[pk]
		}
		getValidatorsFunc = func(ctx context.Context) (map[solana.PublicKey]*solmon.ValidatorView, error) {
			return randomValidators, nil
		}
	} else {
		getValidatorsFunc = func(ctx context.Context) (map[solana.PublicKey]*solmon.ValidatorView, error) {
			return validatorsView.All(), nil
		}
	}

	if *probeDuration > 0 {
		fmt.Printf("\n")
		fmt.Printf("Running public internet probe...\n")
		// Public internet probe
		func() {
			probeCfg := solmon.ProbeConfig{
				Duration: *probeDuration,
				MonitorConfig: solmon.MonitorConfig{
					Logger:               log,
					GetValidatorsFunc:    getValidatorsFunc,
					StatsInterval:        *statsInterval,
					KeepAlivePeriod:      *keepAlivePeriod,
					HandshakeIdleTimeout: *handshakeIdleTimeout,

					Interface: *publicInternetInterface,
				},
				AvailabilityThreshold: *probeAvailabilityThreshold,
			}
			res, err := solmon.RunProbe(ctx, probeCfg)
			if err != nil {
				log.Error("failed to run probe", "error", err)
				os.Exit(1)
			}

			// Print unhealthy validators
			fmt.Printf("\n")
			fmt.Printf("Unhealthy (%d):\n", len(res.Unhealthy))
			// sort.Slice(res.Unhealthy, func(i, j int) bool {
			// 	if res.Unhealthy[i].Health.ConsecutiveFail == res.Unhealthy[j].Health.ConsecutiveFail {
			// 		return res.Unhealthy[i].WindowMeanRTT > res.Unhealthy[j].WindowMeanRTT
			// 	}
			// 	return res.Unhealthy[i].Health.ConsecutiveFail > res.Unhealthy[j].Health.ConsecutiveFail
			// })
			// Sort by pubkey ascending
			sort.Slice(res.Unhealthy, func(i, j int) bool {
				return res.Unhealthy[i].Pubkey.String() < res.Unhealthy[j].Pubkey.String()
			})
			for _, u := range res.Unhealthy {
				fmt.Printf("- %s avail=%.3f ewma=%.3f rtt=%v consecFail=%d\n",
					u.Pubkey.String(),
					u.WindowAvail,
					u.Health.EWMAAvailability,
					u.WindowMeanRTT,
					u.Health.ConsecutiveFail,
				)
			}
			fmt.Printf("\n")

			fmt.Printf("Never available (%d):\n", len(res.NeverAvailable))
			sort.Slice(res.NeverAvailable, func(i, j int) bool {
				return res.NeverAvailable[i].Pubkey.String() < res.NeverAvailable[j].Pubkey.String()
			})
			for _, v := range res.NeverAvailable {
				fmt.Printf("- %s consecFail=%d\n",
					v.Pubkey,
					v.Health.ConsecutiveFail,
				)
			}
			fmt.Printf("\n")

			fmt.Printf("\nBelow threshold (%d) (avail < %.2f):\n", len(res.BelowThreshold), probeCfg.AvailabilityThreshold)
			// sort.Slice(res.BelowThreshold, func(i, j int) bool {
			// 	if res.BelowThreshold[i].Health.ConsecutiveFail == res.BelowThreshold[j].Health.ConsecutiveFail {
			// 		return res.BelowThreshold[i].WindowMeanRTT > res.BelowThreshold[j].WindowMeanRTT
			// 	}
			// 	return res.BelowThreshold[i].Health.ConsecutiveFail > res.BelowThreshold[j].Health.ConsecutiveFail
			// })
			sort.Slice(res.BelowThreshold, func(i, j int) bool {
				return res.BelowThreshold[i].Pubkey.String() < res.BelowThreshold[j].Pubkey.String()
			})
			for _, v := range res.BelowThreshold {
				rtt := v.WindowMeanRTT
				if rtt == 0 && v.HasLatestStats {
					rtt = v.LatestStats.SmoothedRTT
				}
				fmt.Printf("- %s avail=%.3f ewma=%.3f rtt=%v consecFail=%d\n",
					v.Pubkey,
					v.WindowAvail,
					v.Health.EWMAAvailability,
					rtt,
					v.Health.ConsecutiveFail,
				)
			}
		}()

		// DoubleZero probe
		// func() {
		// 	fmt.Printf("\n")
		// 	fmt.Printf("Running doublezero probe...\n")
		// 	probeCfg := solana.ProbeConfig{
		// 		Duration: *probeDuration,
		// 		Config: &solana.Config{
		// 			Logger:               log,
		// 			RPC:                  solanaRPC,
		// 			Interval:             *interval,
		// 			KeepAlivePeriod:      *keepAlivePeriod,
		// 			HandshakeIdleTimeout: *handshakeIdleTimeout,

		// 			Interface: *doublezeroInterface,
		// 		},
		// 		AvailabilityThreshold: *probeAvailabilityThreshold,
		// 		MinTicks:              *probeMinTicks,
		// 	}
		// 	res, err := solana.RunProbe(ctx, probeCfg)
		// 	if err != nil {
		// 		log.Error("failed to run probe", "error", err)
		// 		os.Exit(1)
		// 	}

		// 	// Print unhealthy validators
		// 	fmt.Printf("\n")
		// 	fmt.Printf("Unhealthy (%d):\n", len(res.Unhealthy))
		// 	// sort.Slice(res.Unhealthy, func(i, j int) bool {
		// 	// 	if res.Unhealthy[i].Health.ConsecutiveFail == res.Unhealthy[j].Health.ConsecutiveFail {
		// 	// 		return res.Unhealthy[i].WindowMeanRTT > res.Unhealthy[j].WindowMeanRTT
		// 	// 	}
		// 	// 	return res.Unhealthy[i].Health.ConsecutiveFail > res.Unhealthy[j].Health.ConsecutiveFail
		// 	// })
		// 	// Sort by pubkey ascending
		// 	sort.Slice(res.Unhealthy, func(i, j int) bool {
		// 		return res.Unhealthy[i].Pubkey.String() < res.Unhealthy[j].Pubkey.String()
		// 	})
		// 	for _, u := range res.Unhealthy {
		// 		fmt.Printf("- %s avail=%.3f ewma=%.3f rtt=%v consecFail=%d\n",
		// 			u.Pubkey.String(),
		// 			u.WindowAvail,
		// 			u.Health.EWMAAvailability,
		// 			u.WindowMeanRTT,
		// 			u.Health.ConsecutiveFail,
		// 		)
		// 	}
		// 	fmt.Printf("\n")

		// 	fmt.Printf("Never available (%d):\n", len(res.NeverAvailable))
		// 	sort.Slice(res.NeverAvailable, func(i, j int) bool {
		// 		return res.NeverAvailable[i].Pubkey.String() < res.NeverAvailable[j].Pubkey.String()
		// 	})
		// 	for _, v := range res.NeverAvailable {
		// 		fmt.Printf("- %s consecFail=%d lastFailure=%v\n",
		// 			v.Pubkey,
		// 			v.Health.ConsecutiveFail,
		// 			v.Health.LastFailure,
		// 		)
		// 	}
		// 	fmt.Printf("\n")

		// 	fmt.Printf("\nBelow threshold (%d) (avail < %.2f):\n", len(res.BelowThreshold), probeCfg.AvailabilityThreshold)
		// 	// sort.Slice(res.BelowThreshold, func(i, j int) bool {
		// 	// 	if res.BelowThreshold[i].Health.ConsecutiveFail == res.BelowThreshold[j].Health.ConsecutiveFail {
		// 	// 		return res.BelowThreshold[i].WindowMeanRTT > res.BelowThreshold[j].WindowMeanRTT
		// 	// 	}
		// 	// 	return res.BelowThreshold[i].Health.ConsecutiveFail > res.BelowThreshold[j].Health.ConsecutiveFail
		// 	// })
		// 	sort.Slice(res.BelowThreshold, func(i, j int) bool {
		// 		return res.BelowThreshold[i].Pubkey.String() < res.BelowThreshold[j].Pubkey.String()
		// 	})
		// 	for _, v := range res.BelowThreshold {
		// 		rtt := v.WindowMeanRTT
		// 		if rtt == 0 && v.HasLatestStats {
		// 			rtt = v.LatestStats.SmoothedRTT
		// 		}
		// 		fmt.Printf("- %s avail=%.3f ewma=%.3f rtt=%v consecFail=%d\n",
		// 			v.Pubkey,
		// 			v.WindowAvail,
		// 			v.Health.EWMAAvailability,
		// 			rtt,
		// 			v.Health.ConsecutiveFail,
		// 		)
		// 	}
		// }()
	} else {
		monitor, err := solmon.NewMonitor(&solmon.MonitorConfig{
			Logger:               log,
			GetValidatorsFunc:    getValidatorsFunc,
			StatsInterval:        *statsInterval,
			KeepAlivePeriod:      *keepAlivePeriod,
			HandshakeIdleTimeout: *handshakeIdleTimeout,

			Interface: *publicInternetInterface,
		})
		if err != nil {
			log.Error("failed to create agent", "error", err)
			os.Exit(1)
		}

		go func() {
			err = monitor.Run(ctx)
			if err != nil {
				log.Error("failed to run agent", "error", err)
				cancel()
			}
		}()

		<-ctx.Done()
		log.Info("context done, stopping", "reason", ctx.Err())
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
