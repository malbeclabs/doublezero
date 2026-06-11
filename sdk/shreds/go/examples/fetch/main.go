package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	shreds "github.com/malbeclabs/doublezero/sdk/shreds/go"
)

func main() {
	env := flag.String("env", "mainnet-beta", "Environment: mainnet-beta, testnet, devnet, localnet")
	epoch := flag.Uint64("epoch", 0, "Specific epoch to fetch distribution for (0 = latest settled)")
	flag.Parse()

	validEnvs := map[string]bool{"mainnet-beta": true, "testnet": true, "devnet": true, "localnet": true}
	if !validEnvs[*env] {
		fmt.Fprintf(os.Stderr, "Invalid environment: %s\n", *env)
		os.Exit(1)
	}

	fmt.Printf("Fetching shred subscription data from %s...\n\n", *env)

	client := shreds.NewForEnv(*env)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Program Config
	config, err := client.FetchProgramConfig(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("=== Program Config ===\n")
	fmt.Printf("Admin:               %s\n", config.AdminKey)
	fmt.Printf("Shred Oracle:        %s\n", config.ShredOracleKey)
	fmt.Printf("USDC-2Z Oracle:      %s\n", config.USDC2ZOracleKey)
	fmt.Printf("Max Slippage:        %d bps\n", config.USDC2ZMaxSlippageBps)
	fmt.Printf("Grace Period Slots:  %d\n", config.ClosedForRequestsGracePeriodSlots)
	fmt.Println()

	// Execution Controller
	ec, err := client.FetchExecutionController(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching execution controller: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("=== Execution Controller ===\n")
	fmt.Printf("Current Epoch:       %d\n", ec.CurrentSubscriptionEpoch)
	fmt.Printf("Phase:               %s\n", ec.GetPhase())
	fmt.Printf("Total Metros:        %d\n", ec.TotalMetros)
	fmt.Printf("Enabled Devices:     %d\n", ec.TotalEnabledDevices)
	fmt.Printf("Total Client Seats:  %d\n", ec.TotalClientSeats)
	fmt.Printf("Prices Updated:      %d / %d\n", ec.UpdatedDevicePricesCount, ec.TotalEnabledDevices)
	fmt.Printf("Devices Settled:     %d / %d\n", ec.SettledDevicesCount, ec.TotalEnabledDevices)
	fmt.Printf("Seats Settled:       %d\n", ec.SettledClientSeatsCount)
	fmt.Println()

	// Metro Histories
	metros, err := client.FetchAllMetroHistories(ctx)
	if err != nil {
		fmt.Printf("=== Metro Histories ===\n")
		fmt.Printf("  Error: %v\n\n", err)
	} else {
		fmt.Printf("=== Metro Histories (%d) ===\n", len(metros))
		for _, m := range metros {
			priceStr := "no price"
			if m.Prices.TotalCount > 0 {
				entry := m.Prices.Entries[m.Prices.CurrentIndex]
				priceStr = fmt.Sprintf("$%d (epoch %d)", entry.Price.USDCPriceDollars, entry.Epoch)
			}
			fmt.Printf("  %s: %d devices, %s%s\n",
				m.Pubkey, m.TotalInitializedDevices, priceStr,
				boolTag(m.IsCurrentPriceFinalized(), " [finalized]"))
		}
		fmt.Println()
	}

	// Device Histories
	devices, err := client.FetchAllDeviceHistories(ctx)
	if err != nil {
		fmt.Printf("=== Device Histories ===\n")
		fmt.Printf("  Error: %v\n\n", err)
	} else {
		enabled := 0
		for _, d := range devices {
			if d.IsEnabled() {
				enabled++
			}
		}
		fmt.Printf("=== Device Histories (%d total, %d enabled) ===\n", len(devices), enabled)
		for i, d := range devices {
			if i >= 10 {
				fmt.Printf("  ... and %d more\n", len(devices)-10)
				break
			}
			subStr := "no subscriptions"
			if d.Subscriptions.TotalCount > 0 {
				entry := d.Subscriptions.Entries[d.Subscriptions.CurrentIndex]
				sub := entry.Subscription
				subStr = fmt.Sprintf("seats %d/%d, premium %+d (epoch %d)",
					sub.GrantedSeatCount, sub.TotalAvailableSeats,
					sub.USDCMetroPremiumDollars, entry.Epoch)
			}
			fmt.Printf("  %s: %s%s\n",
				d.Pubkey, subStr,
				boolTag(d.IsEnabled(), " [enabled]"))
		}
		fmt.Println()
	}

	// Client Seats
	seats, err := client.FetchAllClientSeats(ctx)
	if err != nil {
		fmt.Printf("=== Client Seats ===\n")
		fmt.Printf("  Error: %v\n\n", err)
	} else {
		fmt.Printf("=== Client Seats (%d) ===\n", len(seats))
		for i, s := range seats {
			if i >= 10 {
				fmt.Printf("  ... and %d more\n", len(seats)-10)
				break
			}
			ip := ipFromBits(s.ClientIPBits)
			fmt.Printf("  %s: device=%s ip=%s tenure=%d funded_epoch=%d active_epoch=%d escrows=%d\n",
				s.Pubkey, s.DeviceKey.String()[:12]+"...", ip,
				s.TenureEpochs, s.FundedEpoch, s.ActiveEpoch, s.EscrowCount)
		}
		fmt.Println()
	}

	// Payment Escrows
	escrows, err := client.FetchAllPaymentEscrows(ctx)
	if err != nil {
		fmt.Printf("=== Payment Escrows ===\n")
		fmt.Printf("  Error: %v\n\n", err)
	} else {
		totalUSDC := uint64(0)
		for _, e := range escrows {
			totalUSDC += e.USDCBalance
		}
		fmt.Printf("=== Payment Escrows (%d) ===\n", len(escrows))
		fmt.Printf("  Total USDC balance: %d (%.2f USDC)\n", totalUSDC, float64(totalUSDC)/1_000_000)
		for i, e := range escrows {
			if i >= 10 {
				fmt.Printf("  ... and %d more\n", len(escrows)-10)
				break
			}
			fmt.Printf("  %s: seat=%s balance=%.2f USDC\n",
				e.Pubkey, e.ClientSeatKey.String()[:12]+"...",
				float64(e.USDCBalance)/1_000_000)
		}
		fmt.Println()
	}

	// Shred Distribution for a specific epoch
	targetEpoch := *epoch
	if targetEpoch == 0 && ec.CurrentSubscriptionEpoch > 1 {
		targetEpoch = ec.CurrentSubscriptionEpoch - 1
	}
	if targetEpoch > 0 {
		dist, err := client.FetchShredDistribution(ctx, targetEpoch)
		if err != nil {
			fmt.Printf("=== Shred Distribution (epoch %d) ===\n", targetEpoch)
			fmt.Printf("  Not found or error: %v\n", err)
		} else {
			fmt.Printf("=== Shred Distribution (epoch %d) ===\n", dist.SubscriptionEpoch)
			fmt.Printf("  Associated DZ Epoch:   %d\n", dist.AssociatedDZEpoch)
			fmt.Printf("  Devices:               %d\n", dist.DeviceCount)
			fmt.Printf("  Client Seats:          %d\n", dist.ClientSeatCount)
			fmt.Printf("  Collected USDC:        %d (%.2f USDC)\n",
				dist.CollectedUSDCPayments, float64(dist.CollectedUSDCPayments)/1_000_000)
			fmt.Printf("  2Z from USDC:          %d\n", dist.Collected2ZConvertedFromUSDC)
			fmt.Printf("  Publishing Validators: %d\n", dist.TotalPublishingValidators)
			fmt.Printf("  Validator 2Z Dist:     %d\n", dist.DistributedValidator2ZAmount)
			fmt.Printf("  Contributor 2Z Dist:   %d\n", dist.DistributedContributor2ZAmount)
			fmt.Printf("  Burned 2Z:             %d\n", dist.Burned2ZAmount)
		}
		fmt.Println()
	}

	// Validator Client Rewards
	vcrs, err := client.FetchAllValidatorClientRewards(ctx)
	if err != nil {
		fmt.Printf("=== Validator Client Rewards ===\n")
		fmt.Printf("  Error: %v\n\n", err)
	} else {
		fmt.Printf("=== Validator Client Rewards (%d) ===\n", len(vcrs))
		for _, v := range vcrs {
			fmt.Printf("  ID=%d manager=%s desc=%q\n",
				v.ClientID, v.ManagerKey.String()[:12]+"...", v.ShortDescription())
		}
		fmt.Println()
	}

	fmt.Println("Done.")
}

func ipFromBits(bits uint32) string {
	ip := make(net.IP, 4)
	ip[0] = byte(bits)
	ip[1] = byte(bits >> 8)
	ip[2] = byte(bits >> 16)
	ip[3] = byte(bits >> 24)
	return ip.String()
}

func boolTag(cond bool, tag string) string {
	if cond {
		return tag
	}
	return ""
}
