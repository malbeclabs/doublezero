package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	internetdata "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/internet"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/epoch"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

type InternetCmd struct{}

func NewInternetCmd() *InternetCmd {
	return &InternetCmd{}
}

func (c *InternetCmd) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "internet",
		Short: "Get internet latency data",
		RunE: func(cmd *cobra.Command, args []string) error {
			verbose, err := cmd.Root().PersistentFlags().GetBool("verbose")
			if err != nil {
				return fmt.Errorf("failed to get verbose flag: %w", err)
			}
			env, err := cmd.Root().PersistentFlags().GetString("env")
			if err != nil {
				return fmt.Errorf("failed to get env flag: %w", err)
			}
			recency, err := cmd.Flags().GetDuration("recency")
			if err != nil {
				return fmt.Errorf("failed to get recency flag: %w", err)
			}
			rawCSVPath, err := cmd.Flags().GetString("raw-csv")
			if err != nil {
				return fmt.Errorf("failed to get raw-csv flag: %w", err)
			}
			dataProvider, err := cmd.Flags().GetString("data-provider")
			if err != nil {
				return fmt.Errorf("failed to get data-provider flag: %w", err)
			}

			log := newLogger(verbose)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			provider, err := newInternetProvider(log, env)
			if err != nil {
				log.Error("Failed to get provider", "error", err)
				os.Exit(1)
			}

			circuits, err := provider.GetCircuits(ctx)
			if err != nil {
				log.Error("Failed to get circuits", "error", err)
				os.Exit(1)
			}

			from := time.Now().Add(-recency)
			to := time.Now()

			if rawCSVPath != "" {
				file, err := os.Create(rawCSVPath)
				if err != nil {
					log.Error("Failed to create CSV file", "error", err, "path", rawCSVPath)
					os.Exit(1)
				}
				defer file.Close()

				_, err = fmt.Fprintln(file, "circuit,timestamp,rtt_us")
				if err != nil {
					log.Error("Failed to write CSV header", "error", err, "path", rawCSVPath)
					os.Exit(1)
				}

				for _, circuit := range circuits {
					samples, err := provider.GetCircuitLatenciesForTimeRange(ctx, circuit.Code, from, to, dataProvider)
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
							log.Error("Failed to write CSV row", "error", err, "path", rawCSVPath)
							os.Exit(1)
						}
					}
				}
				return nil
			}

			var allStats []stats.CircuitLatencyStat
			for _, circuit := range circuits {
				stats, err := provider.GetCircuitLatenciesDownsampled(
					ctx,
					circuit.Code,
					from,
					to,
					1,
					internetdata.UnitMillisecond,
					dataProvider,
				)
				if err != nil {
					log.Warn("Failed to get circuit latencies", "error", err, "circuit", circuit.Code)
					continue
				}
				allStats = append(allStats, stats...)
			}

			printInternetSummaries(allStats, env, recency)

			return nil
		},
	}

	cmd.Flags().StringP("data-provider", "p", "", "The data provider to query (ripeatlas, whereisup)")
	cmd.Flags().Duration("recency", 24*time.Hour, "Aggregate over the given duration")
	cmd.Flags().String("raw-csv", "", "Path to save raw data to CSV")

	return cmd
}

func newInternetProvider(log *slog.Logger, env string) (internetdata.Provider, error) {
	networkConfig, err := config.NetworkConfigForEnv(env)
	if err != nil {
		return nil, fmt.Errorf("failed to get network config: %w", err)
	}

	rpcClient := solanarpc.New(networkConfig.LedgerPublicRPCURL)

	epochFinder, err := epoch.NewFinder(log, rpcClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create epoch finder: %w", err)
	}

	return internetdata.NewProvider(&internetdata.ProviderConfig{
		Logger:               log,
		ServiceabilityClient: serviceability.New(rpcClient, networkConfig.ServiceabilityProgramID),
		TelemetryClient:      telemetry.New(log, rpcClient, nil, networkConfig.TelemetryProgramID),
		EpochFinder:          epochFinder,
		AgentPK:              networkConfig.InternetLatencyCollectorPK,
	})
}

func printInternetSummaries(stats []stats.CircuitLatencyStat, env string, recency time.Duration) {
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
		"RTT\nP90", "RTT\nP95", "RTT\nP99", "RTT\nMin", "RTT\nMax",
		"RTT\nMedian",
		// "Success\n(#)", "Loss\n(#)", "Loss\n(%)",
		"Count (#)",
	})

	for _, s := range stats {
		table.Append([]string{
			s.Circuit,
			fmt.Sprintf("%.3f", s.RTTMean),
			fmt.Sprintf("%.5f", s.JitterAvg),
			fmt.Sprintf("%.3f", s.JitterEWMA),
			fmt.Sprintf("%.3f", s.JitterMax),
			fmt.Sprintf("%.3f", s.RTTStdDev),
			fmt.Sprintf("%.3f", s.RTTP90),
			fmt.Sprintf("%.3f", s.RTTP95),
			fmt.Sprintf("%.3f", s.RTTP99),
			fmt.Sprintf("%.3f", s.RTTMin),
			fmt.Sprintf("%.3f", s.RTTMax),
			fmt.Sprintf("%.3f", s.RTTMedian),
			fmt.Sprintf("%d", s.SuccessCount),
			// fmt.Sprintf("%d", s.LossCount),
			// fmt.Sprintf("%.1f%%", s.LossRate*100),
		})
	}
	table.Render()
}
