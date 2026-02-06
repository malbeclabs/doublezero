package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

func main() {
	env := flag.String("env", "mainnet-beta", "Environment: mainnet-beta, testnet, devnet, localnet")
	flag.Parse()

	validEnvs := map[string]bool{"mainnet-beta": true, "testnet": true, "devnet": true, "localnet": true}
	if !validEnvs[*env] {
		fmt.Fprintf(os.Stderr, "Invalid environment: %s\n", *env)
		os.Exit(1)
	}

	fmt.Printf("Fetching serviceability data from %s...\n\n", *env)

	client := serviceability.NewForEnv(*env)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data, err := client.GetProgramData(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching program data: %v\n", err)
		os.Exit(1)
	}

	// Summary
	fmt.Printf("=== Summary ===\n")
	fmt.Printf("Locations:       %d\n", len(data.Locations))
	fmt.Printf("Exchanges:       %d\n", len(data.Exchanges))
	fmt.Printf("Contributors:    %d\n", len(data.Contributors))
	fmt.Printf("Devices:         %d\n", len(data.Devices))
	fmt.Printf("Links:           %d\n", len(data.Links))
	fmt.Printf("Users:           %d\n", len(data.Users))
	fmt.Printf("Multicast Groups: %d\n", len(data.MulticastGroups))
	fmt.Printf("Access Passes:   %d\n", len(data.AccessPasses))
	fmt.Println()

	// Global Config
	if data.GlobalConfig != nil {
		fmt.Printf("=== Global Config ===\n")
		fmt.Printf("Local ASN:       %d\n", data.GlobalConfig.LocalASN)
		fmt.Printf("Remote ASN:      %d\n", data.GlobalConfig.RemoteASN)
		fmt.Println()
	}

	// Locations
	if len(data.Locations) > 0 {
		fmt.Printf("=== Locations ===\n")
		for _, loc := range data.Locations {
			fmt.Printf("  %s (%s) - %s [%s]\n",
				loc.Code, loc.Name, loc.Country,
				serviceability.LocationStatus(loc.Status).String())
		}
		fmt.Println()
	}

	// Exchanges
	if len(data.Exchanges) > 0 {
		fmt.Printf("=== Exchanges ===\n")
		for _, ex := range data.Exchanges {
			fmt.Printf("  %s (%s) [%s]\n",
				ex.Code, ex.Name,
				serviceability.ExchangeStatus(ex.Status).String())
		}
		fmt.Println()
	}

	// Devices
	if len(data.Devices) > 0 {
		fmt.Printf("=== Devices ===\n")
		for _, dev := range data.Devices {
			publicIP := fmt.Sprintf("%d.%d.%d.%d", dev.PublicIp[0], dev.PublicIp[1], dev.PublicIp[2], dev.PublicIp[3])
			fmt.Printf("  %s - %s [status=%s, health=%s]\n",
				dev.Code, publicIP,
				serviceability.DeviceStatus(dev.Status).String(),
				serviceability.DeviceHealth(dev.DeviceHealth).String())
		}
		fmt.Println()
	}

	// Links
	if len(data.Links) > 0 {
		fmt.Printf("=== Links ===\n")
		for _, link := range data.Links {
			delayMs := link.DelayNs / 1_000_000
			fmt.Printf("  %s - %s, %d bps, %dms delay [%s]\n",
				link.Code,
				serviceability.LinkLinkType(link.LinkType).String(),
				link.Bandwidth,
				delayMs,
				serviceability.LinkStatus(link.Status).String())
		}
		fmt.Println()
	}

	// Users
	if len(data.Users) > 0 {
		fmt.Printf("=== Users ===\n")
		for _, user := range data.Users {
			ownerPK := solana.PublicKeyFromBytes(user.Owner[:])
			fmt.Printf("  %s... - %s [%s]\n",
				ownerPK.String()[:12],
				serviceability.UserUserType(user.UserType).String(),
				serviceability.UserStatus(user.Status).String())
		}
		fmt.Println()
	}

	fmt.Println("Done.")
}
