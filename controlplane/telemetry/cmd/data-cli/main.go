package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/lmittmann/tint"
	"github.com/olekukonko/tablewriter"

	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/epoch"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

func main() {
	env := flag.String("env", config.EnvDevnet, "The network environment to query (devnet, testnet)")
	recency := flag.Duration("recency", 24*time.Hour, "Aggregate over the given duration")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	rawCSVPath := flag.String("raw-csv", "", "Path to save raw data to CSV")
	flag.Parse()

	log := newLogger(*verbose)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	provider, err := newProvider(log, *env)
	if err != nil {
		log.Error("Failed to get provider", "error", err)
		os.Exit(1)
	}

	circuits, err := provider.GetCircuits(ctx)
	if err != nil {
		log.Error("Failed to get circuits", "error", err)
		os.Exit(1)
	}

	from := time.Now().Add(-*recency)
	to := time.Now()

	if *rawCSVPath != "" {
		file, err := os.Create(*rawCSVPath)
		if err != nil {
			log.Error("Failed to create CSV file", "error", err, "path", *rawCSVPath)
			os.Exit(1)
		}
		defer file.Close()

		_, err = fmt.Fprintln(file, "circuit,timestamp,rtt_us")
		if err != nil {
			log.Error("Failed to write CSV header", "error", err, "path", *rawCSVPath)
			os.Exit(1)
		}

		for _, circuit := range circuits {
			samples, err := provider.GetCircuitLatencies(ctx, circuit.Code, from, to)
			if err != nil {
				log.Error("Failed to get raw data", "error", err, "circuit", circuit.Code)
				os.Exit(1)
			}

			for _, sample := range samples {
				_, err := fmt.Fprintf(file, "%s,%s,%d\n",
					circuit.Code,
					sample.Timestamp, // Already formatted as time.RFC3339Nano
					sample.RTT,
				)
				if err != nil {
					log.Error("Failed to write CSV row", "error", err, "path", *rawCSVPath)
					os.Exit(1)
				}
			}
		}
		return
	}

	var allStats []data.CircuitLatencyStat
	for _, circuit := range circuits {
		stats, err := provider.GetCircuitLatenciesDownsampled(
			ctx,
			circuit.Code,
			from,
			to,
			1,
			data.UnitMillisecond,
		)
		if err != nil {
			log.Warn("Failed to get circuit latencies", "error", err, "circuit", circuit.Code)
			continue
		}
		allStats = append(allStats, stats...)
	}

	printSummaries(allStats, *env, *recency)
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      level,
		TimeFormat: time.Kitchen,
	}))
}

func newProvider(log *slog.Logger, env string) (data.Provider, error) {
	networkConfig, err := config.NetworkConfigForEnv(env)
	if err != nil {
		return nil, fmt.Errorf("failed to get network config: %w", err)
	}

	rpcClient := solanarpc.New(networkConfig.LedgerRPCURL)

	epochFinder, err := epoch.NewFinder(log, rpcClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create epoch finder: %w", err)
	}

	return data.NewProvider(&data.ProviderConfig{
		Logger:               log,
		ServiceabilityClient: serviceability.New(rpcClient, networkConfig.ServiceabilityProgramID),
		TelemetryClient:      telemetry.New(log, rpcClient, nil, networkConfig.TelemetryProgramID),
		EpochFinder:          epochFinder,
	})
}

func printSummaries(stats []data.CircuitLatencyStat, env string, recency time.Duration) {
	fmt.Println("Environment:", env)
	fmt.Println("Recency:", recency)
	fmt.Println("* RTT aggregates are in milliseconds (ms)")

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Timestamp < stats[j].Timestamp
	})

	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_CENTER)
	table.SetAutoFormatHeaders(false)
	table.SetBorder(true)
	table.SetRowLine(true)
	table.SetHeader([]string{
		"Circuit",
		"RTT Mean\n(ms)",
		"Jitter Avg\n(ms)", "Jitter\nEWMA", "Jitter\nMax",
		"RTT\nStdDev",
		"RTT\nP95", "RTT\nP99", "RTT\nMin", "RTT\nMax",
		"RTT\nMedian",
		"Success\n(#)", "Loss\n(#)", "Loss\n(%)",
	})

	for _, s := range stats {
		table.Append([]string{
			s.Circuit,
			fmt.Sprintf("%.3f", s.RTTMean),
			fmt.Sprintf("%.5f", s.JitterAvg),
			fmt.Sprintf("%.3f", s.JitterEWMA),
			fmt.Sprintf("%.3f", s.JitterMax),
			fmt.Sprintf("%.3f", s.RTTStdDev),
			fmt.Sprintf("%.3f", s.RTTP95),
			fmt.Sprintf("%.3f", s.RTTP99),
			fmt.Sprintf("%.3f", s.RTTMin),
			fmt.Sprintf("%.3f", s.RTTMax),
			fmt.Sprintf("%.3f", s.RTTMedian),
			fmt.Sprintf("%d", s.SuccessCount),
			fmt.Sprintf("%d", s.LossCount),
			fmt.Sprintf("%.1f%%", s.LossRate*100),
		})
	}
	table.Render()
}
