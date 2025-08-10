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
	devicedata "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/device"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/epoch"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

type DeviceCmd struct{}

func NewDeviceCmd() *DeviceCmd {
	return &DeviceCmd{}
}

func (c *DeviceCmd) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device",
		Short: "Get device latency data",
		RunE: func(cmd *cobra.Command, args []string) error {
			verbose, err := cmd.Root().PersistentFlags().GetBool("verbose")
			if err != nil {
				return fmt.Errorf("failed to get verbose flag: %w", err)
			}
			env, err := cmd.Root().PersistentFlags().GetString("env")
			if err != nil {
				return fmt.Errorf("failed to get env flag: %w", err)
			}
			recentTime, err := cmd.Flags().GetDuration("recent-time")
			if err != nil {
				return fmt.Errorf("failed to get recent-time flag: %w", err)
			}
			recentEpochs, err := cmd.Flags().GetInt16("recent-epochs")
			if err != nil {
				return fmt.Errorf("failed to get recent-epochs flag: %w", err)
			}
			rawCSVPath, err := cmd.Flags().GetString("raw-csv")
			if err != nil {
				return fmt.Errorf("failed to get raw-csv flag: %w", err)
			}
			unit := devicedata.UnitMillisecond

			log := newLogger(verbose)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			provider, _, err := newDeviceProvider(log, env)
			if err != nil {
				log.Error("Failed to get provider", "error", err)
				os.Exit(1)
			}

			circuits, err := provider.GetCircuits(ctx)
			if err != nil {
				log.Error("Failed to get circuits", "error", err)
				os.Exit(1)
			}

			if recentTime > 0 && recentEpochs != 1 {
				return fmt.Errorf("recent-time and recent-epochs cannot be used together")
			}

			if recentTime > 0 {

			}

			// currentEpoch, err := rpcClient.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
			// if err != nil {
			// 	log.Error("Failed to get current epoch", "error", err)
			// 	os.Exit(1)
			// }

			from := time.Now().Add(-recentTime)
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
					samples, err := provider.GetCircuitLatencies(ctx, devicedata.GetCircuitLatenciesConfig{
						Circuit: circuit.Code,
						Time: &devicedata.TimeRange{
							From: from,
							To:   to,
						},
						Unit: unit,
					})
					if err != nil {
						log.Error("Failed to get raw data", "error", err, "circuit", circuit.Code)
						os.Exit(1)
					}

					for _, sample := range samples {
						_, err := fmt.Fprintf(file, "%s,%s,%d\n",
							circuit.Code,
							sample.Timestamp, // Already formatted as time.RFC3339Nano
							uint32(sample.RTTMean),
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
				stats, err := provider.GetCircuitLatencies(
					ctx,
					devicedata.GetCircuitLatenciesConfig{
						Circuit: circuit.Code,
						Time: &devicedata.TimeRange{
							From: from,
							To:   to,
						},
						MaxPoints: 1,
						Unit:      unit,
					},
				)
				if err != nil {
					log.Warn("Failed to get circuit latencies", "error", err, "circuit", circuit.Code)
					continue
				}
				allStats = append(allStats, stats...)
			}

			printDeviceSummaries(allStats, env, recentTime)

			return nil
		},
	}

	cmd.Flags().Duration("recent-time", 24*time.Hour, "Aggregate over the given duration of time up to now (e.g. 24h)")
	cmd.Flags().Int16("recent-epochs", 1, "Aggregate over the given number of recent epochs")
	cmd.Flags().String("raw-csv", "", "Path to save raw data to CSV")

	return cmd
}

func newDeviceProvider(log *slog.Logger, env string) (devicedata.Provider, *solanarpc.Client, error) {
	networkConfig, err := config.NetworkConfigForEnv(env)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get network config: %w", err)
	}

	rpcClient := solanarpc.New(networkConfig.LedgerPublicRPCURL)

	epochFinder, err := epoch.NewFinder(log, rpcClient)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create epoch finder: %w", err)
	}

	provider, err := devicedata.NewProvider(&devicedata.ProviderConfig{
		Logger:               log,
		ServiceabilityClient: serviceability.New(rpcClient, networkConfig.ServiceabilityProgramID),
		TelemetryClient:      telemetry.New(log, rpcClient, nil, networkConfig.TelemetryProgramID),
		EpochFinder:          epochFinder,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create provider: %w", err)
	}

	return provider, rpcClient, nil
}

func printDeviceSummaries(stats []stats.CircuitLatencyStat, env string, recency time.Duration) {
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
			fmt.Sprintf("%.3f", s.RTTP90),
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
