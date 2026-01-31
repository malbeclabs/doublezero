package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/revdist"
)

func main() {
	solanaEnv := flag.String("solana-env", config.EnvMainnetBeta, "Solana environment (mainnet-beta, testnet, devnet, localnet)")
	programID := flag.String("program-id", "", "Revenue distribution program ID override")
	flag.Usage = printUsage
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	netCfg, err := config.NetworkConfigForEnv(*solanaEnv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid --solana-env %q: %v\n", *solanaEnv, err)
		os.Exit(1)
	}

	rpcURL := netCfg.SolanaRPCURL

	pid := solana.MustPublicKeyFromBase58(config.MainnetRevenueDistributionProgramID)
	if *programID != "" {
		pid = solana.MustPublicKeyFromBase58(*programID)
	}

	rpcClient := solanarpc.New(rpcURL)
	client := revdist.New(rpcClient, pid)
	ctx := context.Background()

	switch args[0] {
	case "config":
		err = cmdConfig(ctx, client, pid)
	case "journal":
		err = cmdJournal(ctx, client)
	case "distribution":
		err = cmdDistribution(ctx, client, args[1:])
	case "deposits":
		err = cmdDeposits(ctx, client, args[1:])
	case "contributors":
		err = cmdContributors(ctx, client, args[1:])
	case "swap-rate":
		err = cmdSwapRate(ctx, netCfg)
	default:
		printUsage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: revdist-cli [flags] <command> [args]

Commands:
  config                       Show program configuration
  journal                      Show journal balances
  distribution [epoch]         Show distribution for epoch (default: latest)
  deposits                     List all validator deposits
  deposits <node_id>           Show single validator deposit
  contributors                 List all contributor rewards
  contributors <service_key>   Show single contributor rewards
  swap-rate                    Show current SOL/2Z oracle swap rate

The revenue distribution program lives on Solana. Use --solana-env to select the
Solana network. The oracle URL is derived from the DZ network config for the
same environment.

Flags:
`)
	flag.PrintDefaults()
}

func cmdConfig(ctx context.Context, client *revdist.Client, programID solana.PublicKey) error {
	cfg, err := client.FetchConfig(ctx)
	if err != nil {
		return err
	}

	addr, _, _ := revdist.DeriveConfigPDA(programID)

	fmt.Printf("Program Config (%s)\n", addr)
	fmt.Printf("%-45s %d\n", "Flags:", cfg.Flags)
	fmt.Printf("%-45s %d\n", "Next Completed DZ Epoch:", cfg.NextCompletedDZEpoch)
	fmt.Printf("%-45s %s\n", "Admin Key:", cfg.AdminKey)
	fmt.Printf("%-45s %s\n", "Debt Accountant Key:", cfg.DebtAccountantKey)
	fmt.Printf("%-45s %s\n", "Rewards Accountant Key:", cfg.RewardsAccountantKey)
	fmt.Printf("%-45s %s\n", "Contributor Manager Key:", cfg.ContributorManagerKey)
	fmt.Printf("%-45s %s\n", "SOL/2Z Swap Program ID:", cfg.SOL2ZSwapProgramID)
	fmt.Printf("%-45s %d\n", "Debt Write-Off Activation Epoch:", cfg.DebtWriteOffFeatureActivationEpoch)

	dp := cfg.DistributionParameters
	fmt.Println()
	fmt.Println("Distribution Parameters:")
	fmt.Printf("  %-43s %d min\n", "Calculation Grace Period:", dp.CalculationGracePeriodMinutes)
	fmt.Printf("  %-43s %d min\n", "Initialization Grace Period:", dp.InitializationGracePeriodMinutes)
	fmt.Printf("  %-43s %d\n", "Min Epoch Duration to Finalize:", dp.MinimumEpochDurationToFinalizeRewards)

	cb := dp.CommunityBurnRateParameters
	fmt.Println()
	fmt.Println("  Community Burn Rate:")
	fmt.Printf("    %-41s %s\n", "Limit:", formatBurnRate(cb.Limit))
	fmt.Printf("    %-41s %d epochs\n", "Epochs to Increasing:", cb.DZEpochsToIncreasing)
	fmt.Printf("    %-41s %d epochs\n", "Epochs to Limit:", cb.DZEpochsToLimit)

	vf := dp.SolanaValidatorFeeParameters
	fmt.Println()
	fmt.Println("  Validator Fee Parameters:")
	fmt.Printf("    %-41s %s\n", "Base Block Rewards:", formatValidatorFee(vf.BaseBlockRewardsPct))
	fmt.Printf("    %-41s %s\n", "Priority Block Rewards:", formatValidatorFee(vf.PriorityBlockRewardsPct))
	fmt.Printf("    %-41s %s\n", "Inflation Rewards:", formatValidatorFee(vf.InflationRewardsPct))
	fmt.Printf("    %-41s %s\n", "Jito Tips:", formatValidatorFee(vf.JitoTipsPct))
	fmt.Printf("    %-41s %d lamports\n", "Fixed SOL Amount:", vf.FixedSOLAmount)

	rp := cfg.RelayParameters
	fmt.Println()
	fmt.Println("  Relay Parameters:")
	fmt.Printf("    %-41s %d lamports\n", "Distribute Rewards:", rp.DistributeRewardsLamports)

	return nil
}

func cmdJournal(ctx context.Context, client *revdist.Client) error {
	journal, err := client.FetchJournal(ctx)
	if err != nil {
		return err
	}

	fmt.Println("Journal")
	fmt.Printf("%-45s %s\n", "Total SOL Balance:", formatSOL(journal.TotalSOLBalance))
	fmt.Printf("%-45s %d\n", "Total 2Z Balance:", journal.Total2ZBalance)
	fmt.Printf("%-45s %d\n", "Swap 2Z Destination Balance:", journal.Swap2ZDestinationBalance)
	fmt.Printf("%-45s %s\n", "Swapped SOL Amount:", formatSOL(journal.SwappedSOLAmount))
	fmt.Printf("%-45s %d\n", "Next DZ Epoch to Sweep Tokens:", journal.NextDZEpochToSweepTokens)
	lifetime := binary.LittleEndian.Uint64(journal.LifetimeSwapped2ZAmount[:8])
	fmt.Printf("%-45s %d\n", "Lifetime Swapped 2Z Amount:", lifetime)

	return nil
}

func cmdDistribution(ctx context.Context, client *revdist.Client, args []string) error {
	var epoch uint64

	if len(args) > 0 {
		e, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid epoch: %w", err)
		}
		epoch = e
	} else {
		cfg, err := client.FetchConfig(ctx)
		if err != nil {
			return fmt.Errorf("fetching config for latest epoch: %w", err)
		}
		if cfg.NextCompletedDZEpoch == 0 {
			return fmt.Errorf("no completed epochs")
		}
		epoch = cfg.NextCompletedDZEpoch - 1
	}

	dist, err := client.FetchDistribution(ctx, epoch)
	if err != nil {
		return err
	}

	fmt.Printf("Distribution — Epoch %d\n", epoch)
	fmt.Printf("%-50s %d\n", "Flags:", dist.Flags)
	fmt.Printf("%-50s %s\n", "Community Burn Rate:", formatBurnRate(dist.CommunityBurnRate))
	fmt.Println()

	fmt.Println("Validator Debt:")
	fmt.Printf("  %-48s %d\n", "Total Validators:", dist.TotalSolanaValidators)
	fmt.Printf("  %-48s %s\n", "Total Debt:", formatSOL(dist.TotalSolanaValidatorDebt))
	fmt.Printf("  %-48s %s\n", "Collected Payments:", formatSOL(dist.CollectedSolanaValidatorPayments))
	fmt.Printf("  %-48s %d\n", "Payments Count:", dist.SolanaValidatorPaymentsCount)
	fmt.Printf("  %-48s %s\n", "Uncollectible Debt:", formatSOL(dist.UncollectibleSOLDebt))
	fmt.Printf("  %-48s %d\n", "Write-Off Count:", dist.SolanaValidatorWriteOffCount)
	fmt.Printf("  %-48s %x\n", "Debt Merkle Root:", dist.SolanaValidatorDebtMerkleRoot)
	fmt.Println()

	fmt.Println("Rewards:")
	fmt.Printf("  %-48s %d\n", "Total Contributors:", dist.TotalContributors)
	fmt.Printf("  %-48s %d\n", "Distributed Rewards Count:", dist.DistributedRewardsCount)
	fmt.Printf("  %-48s %d\n", "Distributed 2Z Amount:", dist.Distributed2ZAmount)
	fmt.Printf("  %-48s %d\n", "Burned 2Z Amount:", dist.Burned2ZAmount)
	fmt.Printf("  %-48s %d\n", "Collected Prepaid 2Z:", dist.CollectedPrepaid2ZPayments)
	fmt.Printf("  %-48s %d\n", "Collected 2Z from SOL:", dist.Collected2ZConvertedFromSOL)
	fmt.Printf("  %-48s %x\n", "Rewards Merkle Root:", dist.RewardsMerkleRoot)
	fmt.Println()

	vf := dist.SolanaValidatorFeeParameters
	fmt.Println("Snapshot — Validator Fees:")
	fmt.Printf("  %-48s %s\n", "Base Block Rewards:", formatValidatorFee(vf.BaseBlockRewardsPct))
	fmt.Printf("  %-48s %s\n", "Priority Block Rewards:", formatValidatorFee(vf.PriorityBlockRewardsPct))
	fmt.Printf("  %-48s %s\n", "Inflation Rewards:", formatValidatorFee(vf.InflationRewardsPct))
	fmt.Printf("  %-48s %s\n", "Jito Tips:", formatValidatorFee(vf.JitoTipsPct))
	fmt.Printf("  %-48s %d lamports\n", "Fixed SOL Amount:", vf.FixedSOLAmount)

	return nil
}

func cmdDeposits(ctx context.Context, client *revdist.Client, args []string) error {
	if len(args) > 0 {
		nodeID := solana.MustPublicKeyFromBase58(args[0])
		deposit, err := client.FetchValidatorDeposit(ctx, nodeID)
		if err != nil {
			return err
		}
		balance, balErr := client.ValidatorDepositBalance(ctx, nodeID)

		fmt.Printf("Validator Deposit — %s\n", nodeID)
		fmt.Printf("%-45s %s\n", "Node ID:", deposit.NodeID)
		fmt.Printf("%-45s %s\n", "Written-Off Debt:", formatSOL(deposit.WrittenOffSOLDebt))
		if balErr == nil {
			fmt.Printf("%-45s %s\n", "Effective Balance:", formatSOL(balance))
		}
		return nil
	}

	deposits, err := client.FetchAllValidatorDeposits(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Validator Deposits (%d total)\n\n", len(deposits))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "NODE ID\tWRITTEN-OFF DEBT\n")
	fmt.Fprintf(w, "-------\t----------------\n")
	for _, d := range deposits {
		fmt.Fprintf(w, "%s\t%s\n", d.NodeID, formatSOL(d.WrittenOffSOLDebt))
	}
	w.Flush()

	return nil
}

func cmdContributors(ctx context.Context, client *revdist.Client, args []string) error {
	if len(args) > 0 {
		serviceKey := solana.MustPublicKeyFromBase58(args[0])
		rewards, err := client.FetchContributorRewards(ctx, serviceKey)
		if err != nil {
			return err
		}

		fmt.Printf("Contributor Rewards — %s\n", serviceKey)
		fmt.Printf("%-45s %s\n", "Rewards Manager:", rewards.RewardsManagerKey)
		fmt.Printf("%-45s %s\n", "Service Key:", rewards.ServiceKey)
		fmt.Printf("%-45s %d\n", "Flags:", rewards.Flags)
		fmt.Println()
		fmt.Println("Recipient Shares:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "  #\tRECIPIENT\tSHARE\n")
		fmt.Fprintf(w, "  -\t---------\t-----\n")
		for i, s := range rewards.RecipientShares {
			if s.RecipientKey.IsZero() {
				continue
			}
			fmt.Fprintf(w, "  %d\t%s\t%s\n", i, s.RecipientKey, formatValidatorFee(s.Share))
		}
		w.Flush()
		return nil
	}

	rewards, err := client.FetchAllContributorRewards(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Contributor Rewards (%d total)\n\n", len(rewards))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "SERVICE KEY\tMANAGER\tFLAGS\tRECIPIENTS\n")
	fmt.Fprintf(w, "-----------\t-------\t-----\t----------\n")
	for _, r := range rewards {
		var recipients []string
		for _, s := range r.RecipientShares {
			if s.RecipientKey.IsZero() {
				continue
			}
			recipients = append(recipients, fmt.Sprintf("%s (%s)", s.RecipientKey, formatValidatorFee(s.Share)))
		}
		recipStr := strings.Join(recipients, ", ")
		if recipStr == "" {
			recipStr = "(none)"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", r.ServiceKey, r.RewardsManagerKey, r.Flags, recipStr)
	}
	w.Flush()

	return nil
}

func cmdSwapRate(ctx context.Context, netCfg *config.NetworkConfig) error {
	oracleURL := netCfg.TwoZOracleURL
	if oracleURL == "" {
		return fmt.Errorf("no oracle URL configured for this environment")
	}

	oracle := revdist.NewOracleClient(oracleURL)
	rate, err := oracle.FetchSwapRate(ctx)
	if err != nil {
		return err
	}

	fmt.Println("SOL/2Z Oracle Swap Rate")
	fmt.Printf("%-45s %.0f\n", "Swap Rate:", rate.Rate)
	fmt.Printf("%-45s %s\n", "SOL Price (USD):", rate.SOLPriceUSD)
	fmt.Printf("%-45s %s\n", "2Z Price (USD):", rate.TwoZPriceUSD)
	fmt.Printf("%-45s %d\n", "Timestamp:", rate.Timestamp)
	fmt.Printf("%-45s %v\n", "Cache Hit:", rate.CacheHit)

	return nil
}

func formatSOL(lamports uint64) string {
	sol := float64(lamports) / 1e9
	return fmt.Sprintf("%.9f SOL (%d lamports)", sol, lamports)
}

func formatBurnRate(rate uint32) string {
	pct := float64(rate) / 1e7
	return fmt.Sprintf("%.2f%% (%d/1000000000)", pct, rate)
}

func formatValidatorFee(fee uint16) string {
	pct := float64(fee) / 100
	return fmt.Sprintf("%.2f%% (%d/10000)", pct, fee)
}
