package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	revdist "github.com/malbeclabs/doublezero/sdk/revdist/go"
)

func main() {
	env := flag.String("env", "mainnet-beta", "Environment: mainnet-beta, testnet, devnet, localnet")
	epoch := flag.Uint64("epoch", 0, "Specific epoch to fetch distribution for (0 = use latest from config)")
	flag.Parse()

	validEnvs := map[string]bool{"mainnet-beta": true, "testnet": true, "devnet": true, "localnet": true}
	if !validEnvs[*env] {
		fmt.Fprintf(os.Stderr, "Invalid environment: %s\n", *env)
		os.Exit(1)
	}

	fmt.Printf("Fetching revenue distribution data from %s...\n\n", *env)

	client := revdist.NewForEnv(*env)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fetch program config
	config, err := client.FetchConfig(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== Program Config ===\n")
	fmt.Printf("Admin:                  %s\n", config.AdminKey)
	fmt.Printf("Debt Accountant:        %s\n", config.DebtAccountantKey)
	fmt.Printf("Rewards Accountant:     %s\n", config.RewardsAccountantKey)
	fmt.Printf("Contributor Manager:    %s\n", config.ContributorManagerKey)
	fmt.Printf("Next Completed Epoch:   %d\n", config.NextCompletedDZEpoch)
	fmt.Println()

	fmt.Printf("=== Distribution Parameters ===\n")
	fmt.Printf("Calculation Grace Period:   %d minutes\n", config.DistributionParameters.CalculationGracePeriodMinutes)
	fmt.Printf("Initialization Grace:       %d minutes\n", config.DistributionParameters.InitializationGracePeriodMinutes)
	fmt.Printf("Min Epoch Duration:         %d\n", config.DistributionParameters.MinimumEpochDurationToFinalizeRewards)
	fmt.Println()

	fmt.Printf("=== Validator Fee Parameters ===\n")
	fmt.Printf("Base Block Rewards:     %.2f%%\n", float64(config.DistributionParameters.SolanaValidatorFeeParameters.BaseBlockRewardsPct)/100)
	fmt.Printf("Priority Block Rewards: %.2f%%\n", float64(config.DistributionParameters.SolanaValidatorFeeParameters.PriorityBlockRewardsPct)/100)
	fmt.Printf("Inflation Rewards:      %.2f%%\n", float64(config.DistributionParameters.SolanaValidatorFeeParameters.InflationRewardsPct)/100)
	fmt.Printf("Jito Tips:              %.2f%%\n", float64(config.DistributionParameters.SolanaValidatorFeeParameters.JitoTipsPct)/100)
	fmt.Println()

	// Fetch distribution for a specific epoch
	targetEpoch := *epoch
	if targetEpoch == 0 && config.NextCompletedDZEpoch > 0 {
		targetEpoch = config.NextCompletedDZEpoch - 1
	}

	if targetEpoch > 0 {
		dist, err := client.FetchDistribution(ctx, targetEpoch)
		if err != nil {
			fmt.Printf("=== Distribution (epoch %d) ===\n", targetEpoch)
			fmt.Printf("  Not found or error: %v\n", err)
		} else {
			fmt.Printf("=== Distribution (epoch %d) ===\n", dist.DZEpoch)
			fmt.Printf("Community Burn Rate:            %d (%.2f%%)\n",
				dist.CommunityBurnRate,
				float64(dist.CommunityBurnRate)/1_000_000_000*100)
			fmt.Printf("Total Solana Validators:        %d\n", dist.TotalSolanaValidators)
			fmt.Printf("Validator Payments Count:       %d\n", dist.SolanaValidatorPaymentsCount)
			fmt.Printf("Total Validator Debt:           %d lamports\n", dist.TotalSolanaValidatorDebt)
			fmt.Printf("Collected Validator Payments:   %d lamports\n", dist.CollectedSolanaValidatorPayments)
			fmt.Printf("Total Contributors:             %d\n", dist.TotalContributors)
			fmt.Printf("Distributed Rewards Count:      %d\n", dist.DistributedRewardsCount)
			fmt.Printf("Collected Prepaid 2Z:           %d\n", dist.CollectedPrepaid2ZPayments)
			fmt.Printf("2Z Converted from SOL:          %d\n", dist.Collected2ZConvertedFromSOL)
			fmt.Printf("Distributed 2Z Amount:          %d\n", dist.Distributed2ZAmount)
		}
		fmt.Println()
	}

	// Fetch journal
	journal, err := client.FetchJournal(ctx)
	if err != nil {
		fmt.Printf("=== Journal ===\n")
		fmt.Printf("  Not found or error: %v\n", err)
	} else {
		fmt.Printf("=== Journal ===\n")
		fmt.Printf("Total SOL Balance:          %d lamports\n", journal.TotalSOLBalance)
		fmt.Printf("Total 2Z Balance:           %d\n", journal.Total2ZBalance)
		fmt.Printf("Swapped SOL Amount:         %d lamports\n", journal.SwappedSOLAmount)
		fmt.Printf("Next Epoch to Sweep:        %d\n", journal.NextDZEpochToSweepTokens)
	}
	fmt.Println()

	// Fetch all validator deposits
	deposits, err := client.FetchAllValidatorDeposits(ctx)
	if err != nil {
		fmt.Printf("=== Validator Deposits ===\n")
		fmt.Printf("  Error: %v\n", err)
	} else {
		fmt.Printf("=== Validator Deposits (%d) ===\n", len(deposits))
		for i, dep := range deposits {
			if i >= 10 {
				fmt.Printf("  ... and %d more\n", len(deposits)-10)
				break
			}
			nodeID := solana.PublicKeyFromBytes(dep.NodeID[:])
			fmt.Printf("  %s: written off debt %d\n", nodeID.String()[:16]+"...", dep.WrittenOffSOLDebt)
		}
	}
	fmt.Println()

	// Fetch all contributor rewards
	rewards, err := client.FetchAllContributorRewards(ctx)
	if err != nil {
		fmt.Printf("=== Contributor Rewards ===\n")
		fmt.Printf("  Error: %v\n", err)
	} else {
		fmt.Printf("=== Contributor Rewards (%d) ===\n", len(rewards))
		for i, r := range rewards {
			if i >= 10 {
				fmt.Printf("  ... and %d more\n", len(rewards)-10)
				break
			}
			serviceKey := solana.PublicKeyFromBytes(r.ServiceKey[:])
			fmt.Printf("  %s: rewards manager %s\n", serviceKey.String()[:16]+"...", r.RewardsManagerKey.String()[:16]+"...")
		}
	}
	fmt.Println()

	fmt.Println("Done.")
}
