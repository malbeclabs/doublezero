package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/lmittmann/tint"
	"github.com/olekukonko/tablewriter"

	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

type Summary struct {
	Origin, Target, Link string
	TotalRTT             uint64
	SuccessCount         uint64
	LossCount            uint64
	LossRate             float64
	Avg                  float64
	Median               float64
	Jitter               float64
	MAD                  float64
	P95                  float64
	P99                  float64
	Min                  float64
	Max                  float64
}

type Config struct {
	RPCEndpoint             string
	ServiceabilityProgramID string
	TelemetryProgramID      string
	Epoch                   uint64
	RecentEpochs            uint64
	Verbose                 bool
}

func main() {
	cfg := parseFlags()
	log := newLogger(cfg.Verbose)

	if cfg.Epoch == 0 {
		cfg.Epoch = deriveEpoch(time.Now().UTC())
	}

	summaries, err := buildSummaries(
		context.Background(), log,
		cfg.RPCEndpoint, cfg.ServiceabilityProgramID, cfg.TelemetryProgramID,
		cfg.Epoch, cfg.RecentEpochs,
	)
	if err != nil {
		log.Error("Failed to build summaries", "error", err)
		os.Exit(1)
	}

	printSummaries(summaries, cfg)
}

func parseFlags() *Config {
	cfg := &Config{}
	flag.StringVar(&cfg.RPCEndpoint, "rpc-endpoint", dzsdk.DZ_LEDGER_RPC_URL, "Solana RPC endpoint")
	flag.StringVar(&cfg.ServiceabilityProgramID, "serviceability-program-id", serviceability.SERVICEABILITY_PROGRAM_ID_DEVNET, "Serviceability program ID")
	flag.StringVar(&cfg.TelemetryProgramID, "telemetry-program-id", telemetry.TELEMETRY_PROGRAM_ID_DEVNET, "Telemetry program ID")
	flag.Uint64Var(&cfg.Epoch, "epoch", 0, "Epoch to query (0 for current epoch)")
	flag.Uint64Var(&cfg.RecentEpochs, "recent-epochs", 1, "Aggregate over the last N epochs ending at --epoch")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose logging")
	flag.Parse()
	return cfg
}

func deriveEpoch(now time.Time) uint64 {
	return uint64(now.Unix() / (60 * 60 * 24 * 2))
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

func printSummaries(summaries []*Summary, cfg *Config) {
	start := cfg.Epoch - cfg.RecentEpochs + 1
	epochLabel := fmt.Sprintf("Epochs: %d–%d", start, cfg.Epoch)
	if cfg.RecentEpochs == 1 {
		epochLabel = fmt.Sprintf("Epoch: %d", cfg.Epoch)
	}

	fmt.Println(epochLabel)
	fmt.Println("Telemetry Program:", cfg.TelemetryProgramID)
	fmt.Println("Serviceability Program:", cfg.ServiceabilityProgramID)
	fmt.Println("* RTT aggregates are in microseconds (µs)")

	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Origin != summaries[j].Origin {
			return summaries[i].Origin < summaries[j].Origin
		}
		if summaries[i].Target != summaries[j].Target {
			return summaries[i].Target < summaries[j].Target
		}
		return summaries[i].Link < summaries[j].Link
	})

	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_CENTER)
	table.SetAutoFormatHeaders(false)
	table.SetBorder(true)
	table.SetRowLine(true)
	table.SetHeader([]string{
		"Origin", "Target", "Link",
		"RTT Mean\n(µs)", "Median\n(µs)", "Jitter\n(µs)", "MAD",
		"P95", "P99", "Min", "Max",
		"Success\n(#)", "Loss\n(#)", "Loss\n(%)",
	})

	prevOrigin := ""
	for _, s := range summaries {
		origin := s.Origin
		if origin == prevOrigin {
			origin = ""
		}
		prevOrigin = s.Origin
		table.Append([]string{
			origin, s.Target, s.Link,
			fmt.Sprintf("%.0f", s.Avg),
			fmt.Sprintf("%.0f", s.Median),
			fmt.Sprintf("%.1f", s.Jitter),
			fmt.Sprintf("%.1f", s.MAD),
			fmt.Sprintf("%.0f", s.P95),
			fmt.Sprintf("%.0f", s.P99),
			fmt.Sprintf("%.0f", s.Min),
			fmt.Sprintf("%.0f", s.Max),
			fmt.Sprintf("%d", s.SuccessCount),
			fmt.Sprintf("%d", s.LossCount),
			fmt.Sprintf("%.1f%%", s.LossRate),
		})
	}
	table.Render()
}

func buildSummaries(ctx context.Context, log *slog.Logger, rpcEndpoint, serviceabilityProgramID, telemetryProgramID string, epoch, recent uint64) ([]*Summary, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rpcClient := rpc.New(rpcEndpoint)
	svcPID, err := solana.PublicKeyFromBase58(serviceabilityProgramID)
	if err != nil {
		return nil, fmt.Errorf("invalid serviceability program ID: %w", err)
	}
	telPID, err := solana.PublicKeyFromBase58(telemetryProgramID)
	if err != nil {
		return nil, fmt.Errorf("invalid telemetry program ID: %w", err)
	}

	svc := serviceability.New(rpcClient, svcPID)
	tel := telemetry.New(log, rpcClient, nil, telPID)

	if err := svc.Load(ctx); err != nil {
		return nil, fmt.Errorf("failed to load serviceability data: %w", err)
	}

	devices := map[solana.PublicKey]*serviceability.Device{}
	for _, d := range svc.GetDevices() {
		devices[solana.PublicKeyFromBytes(d.PubKey[:])] = &d
	}

	type groupKey struct {
		origin, target, link string
	}
	var groupsMu sync.Mutex
	groups := make(map[groupKey][]uint32)

	for _, link := range svc.GetLinks() {
		linkPK := solana.PublicKeyFromBytes(link.PubKey[:])
		sideA := solana.PublicKeyFromBytes(link.SideAPubKey[:])
		sideZ := solana.PublicKeyFromBytes(link.SideZPubKey[:])

		type query struct {
			origin, target *serviceability.Device
			epoch          uint64
		}

		queries := []query{}
		for _, dir := range []struct{ origin, target solana.PublicKey }{
			{sideA, sideZ}, {sideZ, sideA},
		} {
			origin, target := devices[dir.origin], devices[dir.target]
			if origin == nil || target == nil {
				continue
			}
			for e := int64(epoch) - int64(recent) + 1; e <= int64(epoch); e++ {
				queries = append(queries, query{
					origin: origin,
					target: target,
					epoch:  uint64(e),
				})
			}
		}

		wg := sync.WaitGroup{}
		errChan := make(chan error, len(queries))
		wg.Add(len(queries))
		for _, q := range queries {
			go func(q query) {
				defer wg.Done()

				log.Debug("Fetching device latency samples", "origin", q.origin.Code, "target", q.target.Code, "link", link.Code, "epoch", q.epoch)

				account, err := tel.GetDeviceLatencySamples(ctx, q.origin.PubKey, q.target.PubKey, linkPK, q.epoch)
				if err != nil {
					errChan <- fmt.Errorf("failed to get device latency samples: %w", err)
					return
				}

				for e := int64(epoch) - int64(recent) + 1; e <= int64(epoch); e++ {
					groupsMu.Lock()
					groupKey := groupKey{
						origin: q.origin.Code,
						target: q.target.Code,
						link:   link.Code,
					}
					if _, ok := groups[groupKey]; !ok {
						groups[groupKey] = []uint32{}
					}
					groups[groupKey] = append(groups[groupKey], account.Samples...)
					groupsMu.Unlock()
				}
			}(q)
		}
		wg.Wait()
		close(errChan)
		for err := range errChan {
			return nil, err
		}
	}

	summaries := make([]*Summary, 0, len(groups))

	for key, samples := range groups {
		summary := &Summary{
			Origin: key.origin,
			Target: key.target,
			Link:   key.link,
		}

		successSamples := make([]uint32, 0, len(samples))
		for _, rtt := range samples {
			if rtt > 0 {
				summary.TotalRTT += uint64(rtt)
				summary.SuccessCount++
				successSamples = append(successSamples, rtt)
			} else {
				summary.LossCount++
			}
		}

		if summary.SuccessCount > 0 {
			summary.LossRate, summary.Avg, summary.Median, summary.Jitter, summary.MAD, summary.P95, summary.P99, summary.Min, summary.Max =
				computeStats(successSamples, summary.TotalRTT, summary.SuccessCount, summary.LossCount)
		}

		summaries = append(summaries, summary)
	}

	return summaries, nil
}

func computeStats(samples []uint32, totalRTT, successCount, lossCount uint64) (lossRate, avg, median, jitter, mad, p95, p99, min, max float64) {
	n := len(samples)
	if n == 0 {
		return
	}
	slices.Sort(samples)

	lossRate = 100 * float64(lossCount) / float64(successCount+lossCount)
	avg = float64(totalRTT) / float64(successCount)
	median = float64(samples[n/2])
	min, max = float64(samples[0]), float64(samples[n-1])

	if n >= 20 {
		p95 = float64(samples[int(0.95*float64(n))])
	} else {
		p95 = max
	}
	if n >= 100 {
		p99 = float64(samples[int(0.99*float64(n))])
	} else {
		p99 = max
	}

	for _, s := range samples {
		diff := float64(s) - avg
		jitter += diff * diff
	}
	jitter = math.Sqrt(jitter / float64(n))

	abs := make([]float64, n)
	for i, s := range samples {
		abs[i] = math.Abs(float64(s) - median)
	}
	sort.Float64s(abs)
	mad = abs[n/2]
	return
}
