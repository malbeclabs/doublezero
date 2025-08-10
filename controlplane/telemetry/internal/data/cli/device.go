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
			recentEpochs, err := cmd.Flags().GetInt32("recent-epochs")
			if err != nil {
				return fmt.Errorf("failed to get recent-epochs flag: %w", err)
			}
			epoch, err := cmd.Flags().GetInt32("epoch")
			if err != nil {
				return fmt.Errorf("failed to get epoch flag: %w", err)
			}
			fromEpoch, err := cmd.Flags().GetInt32("from-epoch")
			if err != nil {
				return fmt.Errorf("failed to get from-epoch flag: %w", err)
			}
			toEpoch, err := cmd.Flags().GetInt32("to-epoch")
			if err != nil {
				return fmt.Errorf("failed to get to-epoch flag: %w", err)
			}
			rawCSVPath, err := cmd.Flags().GetString("raw-csv")
			if err != nil {
				return fmt.Errorf("failed to get raw-csv flag: %w", err)
			}
			unitStr, err := cmd.Flags().GetString("unit")
			if err != nil {
				return fmt.Errorf("failed to get unit flag: %w", err)
			}

			var unit devicedata.Unit
			switch unitStr {
			case "ms":
				unit = devicedata.UnitMillisecond
			case "us":
				unit = devicedata.UnitMicrosecond
			default:
				return fmt.Errorf("invalid unit: %s", unitStr)
			}

			log := newLogger(verbose)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			provider, rpcClient, err := newDeviceProvider(log, env)
			if err != nil {
				log.Error("Failed to get provider", "error", err)
				os.Exit(1)
			}

			circuits, err := provider.GetCircuits(ctx)
			if err != nil {
				log.Error("Failed to get circuits", "error", err)
				os.Exit(1)
			}

			usingTimeWindow := recentTime > 0
			usingEpochWindow := recentEpochs > 1
			usingExactEpoch := epoch != 0
			usingEpochRange := fromEpoch != 0 || toEpoch != 0
			selectors := 0
			if usingTimeWindow {
				selectors++
			}
			if usingEpochWindow {
				selectors++
			}
			if usingExactEpoch {
				selectors++
			}
			if usingEpochRange {
				selectors++
			}
			if selectors > 1 {
				return fmt.Errorf("specify only one of: recent-time, recent-epochs, epoch, or from/to epoch range")
			}
			if usingEpochRange && fromEpoch != 0 && toEpoch != 0 && fromEpoch > toEpoch {
				return fmt.Errorf("from-epoch must be less than to-epoch")
			}

			var timeRange *devicedata.TimeRange
			var epochRange *devicedata.EpochRange
			switch {
			case usingTimeWindow:
				now := time.Now().UTC()
				timeRange = &devicedata.TimeRange{From: now.Add(-recentTime), To: now}
			case usingExactEpoch:
				e := uint64(epoch)
				epochRange = &devicedata.EpochRange{From: e, To: e}
			case usingEpochWindow:
				epochInfo, err := rpcClient.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
				if err != nil {
					log.Error("failed to get current epoch", "error", err)
					os.Exit(1)
				}
				cur := epochInfo.Epoch
				n := uint64(recentEpochs - 1)
				from := uint64(0)
				if cur >= n {
					from = cur - n
				}
				epochRange = &devicedata.EpochRange{From: from, To: cur}
			case usingEpochRange:
				epochRange = &devicedata.EpochRange{From: uint64(fromEpoch), To: uint64(toEpoch)}
			default:
				epochInfo, err := rpcClient.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
				if err != nil {
					log.Error("failed to get current epoch", "error", err)
					os.Exit(1)
				}
				cur := epochInfo.Epoch
				epochRange = &devicedata.EpochRange{From: cur, To: cur}
			}

			if rawCSVPath != "" {
				file, err := os.Create(rawCSVPath)
				if err != nil {
					log.Error("Failed to create CSV file", "error", err, "path", rawCSVPath)
					os.Exit(1)
				}
				defer file.Close()

				_, err = fmt.Fprintln(file, "circuit,timestamp,rtt_"+string(unit))
				if err != nil {
					log.Error("Failed to write CSV header", "error", err, "path", rawCSVPath)
					os.Exit(1)
				}

				for _, circuit := range circuits {
					samples, err := provider.GetCircuitLatencies(ctx, devicedata.GetCircuitLatenciesConfig{
						Circuit: circuit.Code,
						Time:    timeRange,
						Epochs:  epochRange,
						Unit:    unit,
					})
					if err != nil {
						log.Error("Failed to get raw data", "error", err, "circuit", circuit.Code)
						os.Exit(1)
					}

					for _, sample := range samples {
						_, err := fmt.Fprintf(file, "%s,%s,%f\n",
							circuit.Code,
							sample.Timestamp, // Already formatted as time.RFC3339Nano
							sample.RTTMean,
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
						Circuit:   circuit.Code,
						Time:      timeRange,
						Epochs:    epochRange,
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

			printDeviceSummaries(allStats, env, recentTime, epochRange, unit)

			return nil
		},
	}

	cmd.Flags().Duration("recent-time", 0, "Aggregate over the given duration up to now (e.g. 24h)")
	cmd.Flags().Int32("recent-epochs", 1, "Aggregate over the given number of recent epochs")
	cmd.Flags().Int32("epoch", 0, "Aggregate over the given epoch")
	cmd.Flags().Int32("from-epoch", 0, "Aggregate from the given epoch")
	cmd.Flags().Int32("to-epoch", 0, "Aggregate to the given epoch")
	cmd.Flags().String("raw-csv", "", "Path to save raw data to CSV")
	cmd.Flags().String("unit", "ms", "Unit to display latencies in (ms, us)")

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

func printDeviceSummaries(stats []stats.CircuitLatencyStat, env string, recentTime time.Duration, epochRange *devicedata.EpochRange, unit devicedata.Unit) {
	fmt.Println("Environment:", env)
	if recentTime > 0 {
		fmt.Println("Recent time:", recentTime)
	}
	if epochRange != nil {
		if epochRange.From == epochRange.To {
			fmt.Println("Epoch:", epochRange.From)
		} else {
			fmt.Println("Epochs:", epochRange.From, "-", epochRange.To)
		}
	}
	fmt.Println("* RTT aggregates are in", unit)

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
		"RTT Mean\n(" + string(unit) + ")",
		"Jitter Avg\n(" + string(unit) + ")", "Jitter\nEWMA", "Jitter\nMax",
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
