package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	internetdata "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/internet"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	telemetry "github.com/malbeclabs/doublezero/sdk/telemetry/go"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/epoch"
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
			dataProvider, err := cmd.Flags().GetString("data-provider")
			if err != nil {
				return fmt.Errorf("failed to get data-provider flag: %w", err)
			}
			unitStr, err := cmd.Flags().GetString("unit")
			if err != nil {
				return fmt.Errorf("failed to get unit flag: %w", err)
			}

			var unit internetdata.Unit
			switch unitStr {
			case "ms":
				unit = internetdata.UnitMillisecond
			case "us":
				unit = internetdata.UnitMicrosecond
			default:
				return fmt.Errorf("invalid unit: %s", unitStr)
			}

			if dataProvider == "" {
				dataProvider = internetdata.DataProviderNameWheresitup
			}
			switch dataProvider {
			case internetdata.DataProviderNameRIPEAtlas, internetdata.DataProviderNameWheresitup:
			default:
				return fmt.Errorf("invalid data provider: %s", dataProvider)
			}

			log := newLogger(verbose)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			provider, rpcClient, err := newInternetProvider(log, env)
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

			var timeRange *internetdata.TimeRange
			var epochRange *internetdata.EpochRange
			switch {
			case usingTimeWindow:
				now := time.Now().UTC()
				timeRange = &internetdata.TimeRange{From: now.Add(-recentTime), To: now}
			case usingExactEpoch:
				e := uint64(epoch)
				epochRange = &internetdata.EpochRange{From: e, To: e}
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
				epochRange = &internetdata.EpochRange{From: from, To: cur}
			case usingEpochRange:
				epochRange = &internetdata.EpochRange{From: uint64(fromEpoch), To: uint64(toEpoch)}
			default:
				epochInfo, err := rpcClient.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
				if err != nil {
					log.Error("failed to get current epoch", "error", err)
					os.Exit(1)
				}
				cur := epochInfo.Epoch
				epochRange = &internetdata.EpochRange{From: cur, To: cur}
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

				var wg sync.WaitGroup

				for _, circuit := range circuits {
					wg.Add(1)
					go func(circuit internetdata.Circuit) {
						defer wg.Done()
						samples, err := provider.GetCircuitLatencies(ctx, internetdata.GetCircuitLatenciesConfig{
							Circuit:      circuit.Code,
							Time:         timeRange,
							Epochs:       epochRange,
							Unit:         unit,
							DataProvider: dataProvider,
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
					}(circuit)
				}
				wg.Wait()
				return nil
			}

			var allStats []stats.CircuitLatencyStat
			var wg sync.WaitGroup
			for _, circuit := range circuits {
				wg.Add(1)
				go func(circuit internetdata.Circuit) {
					defer wg.Done()
					stats, err := provider.GetCircuitLatencies(
						ctx,
						internetdata.GetCircuitLatenciesConfig{
							Circuit:      circuit.Code,
							Time:         timeRange,
							Epochs:       epochRange,
							Unit:         unit,
							MaxPoints:    1,
							DataProvider: dataProvider,
						},
					)
					if err != nil {
						log.Warn("Failed to get circuit latencies", "error", err, "circuit", circuit.Code)
						return
					}
					allStats = append(allStats, stats...)
				}(circuit)
			}
			wg.Wait()

			printInternetSummaries(allStats, env, dataProvider, recentTime, epochRange, unit)

			return nil
		},
	}

	cmd.Flags().StringP("data-provider", "p", "", "The data provider to query (ripeatlas, whereisup)")
	cmd.Flags().Duration("recent-time", 0, "Aggregate over the given duration up to now (e.g. 24h)")
	cmd.Flags().Int32("recent-epochs", 1, "Aggregate over the given number of recent epochs")
	cmd.Flags().Int32("epoch", 0, "Aggregate over the given epoch")
	cmd.Flags().Int32("from-epoch", 0, "Aggregate from the given epoch")
	cmd.Flags().Int32("to-epoch", 0, "Aggregate to the given epoch")
	cmd.Flags().String("raw-csv", "", "Path to save raw data to CSV")
	cmd.Flags().String("unit", "ms", "Unit to display latencies in (ms, us)")

	return cmd
}

func newInternetProvider(log *slog.Logger, env string) (internetdata.Provider, *solanarpc.Client, error) {
	networkConfig, err := config.NetworkConfigForEnv(env)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get network config: %w", err)
	}

	rpcClient := solanarpc.New(networkConfig.LedgerPublicRPCURL)

	epochFinder, err := epoch.NewFinder(log, rpcClient)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create epoch finder: %w", err)
	}

	provider, err := internetdata.NewProvider(&internetdata.ProviderConfig{
		Logger:               log,
		ServiceabilityClient: serviceability.New(rpcClient, networkConfig.ServiceabilityProgramID),
		TelemetryClient:      telemetry.New(rpcClient, networkConfig.TelemetryProgramID),
		EpochFinder:          epochFinder,
		AgentPK:              networkConfig.InternetLatencyCollectorPK,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create provider: %w", err)
	}

	return provider, rpcClient, nil
}

func printInternetSummaries(stats []stats.CircuitLatencyStat, env string, dataProvider string, recentTime time.Duration, epochRange *internetdata.EpochRange, unit internetdata.Unit) {
	fmt.Println("Environment:", env)
	fmt.Println("Data provider:", dataProvider)
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
		if stats[i].Circuit == stats[j].Circuit {
			return stats[i].Timestamp < stats[j].Timestamp
		}
		return stats[i].Circuit < stats[j].Circuit
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
