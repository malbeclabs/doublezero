package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	telemetry "github.com/malbeclabs/doublezero/sdk/telemetry/go"
)

func main() {
	env := flag.String("env", "mainnet-beta", "Environment: mainnet-beta, testnet, devnet, localnet")
	epoch := flag.Uint64("epoch", 0, "Epoch to fetch samples for (0 = try recent epochs)")
	flag.Parse()

	validEnvs := map[string]bool{"mainnet-beta": true, "testnet": true, "devnet": true, "localnet": true}
	if !validEnvs[*env] {
		fmt.Fprintf(os.Stderr, "Invalid environment: %s\n", *env)
		os.Exit(1)
	}

	fmt.Printf("Fetching telemetry data from %s...\n\n", *env)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// First, get serviceability data to discover devices and links
	svcClient := serviceability.NewForEnv(*env)
	svcData, err := svcClient.GetProgramData(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching serviceability data: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== Network Overview ===\n")
	fmt.Printf("Devices: %d\n", len(svcData.Devices))
	fmt.Printf("Links:   %d\n", len(svcData.Links))
	fmt.Println()

	if len(svcData.Links) == 0 {
		fmt.Println("No links found - no telemetry data to fetch.")
		return
	}

	// Build device code map for display
	deviceCodes := make(map[string]string)
	for _, dev := range svcData.Devices {
		pk := solana.PublicKeyFromBytes(dev.PubKey[:])
		deviceCodes[pk.String()] = dev.Code
	}

	// Create telemetry client
	telClient := telemetry.NewForEnv(*env)

	// Determine which epoch to try
	targetEpoch := *epoch
	if targetEpoch == 0 {
		// Get current epoch from DZ Ledger RPC
		ledgerRPC := rpc.New(telemetry.LedgerRPCURLs[*env])
		epochInfo, err := ledgerRPC.GetEpochInfo(ctx, rpc.CommitmentFinalized)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting epoch info: %v\n", err)
			os.Exit(1)
		}
		targetEpoch = epochInfo.Epoch
	}

	fmt.Printf("=== Device Latency Samples (epoch %d) ===\n", targetEpoch)

	samplesFound := 0
	for _, link := range svcData.Links {
		sideAPK := solana.PublicKeyFromBytes(link.SideAPubKey[:])
		sideZPK := solana.PublicKeyFromBytes(link.SideZPubKey[:])
		linkPK := solana.PublicKeyFromBytes(link.PubKey[:])

		sideACode := deviceCodes[sideAPK.String()]
		sideZCode := deviceCodes[sideZPK.String()]

		// Try both directions
		for _, dir := range []struct {
			origin, target solana.PublicKey
			oCode, tCode   string
		}{
			{sideAPK, sideZPK, sideACode, sideZCode},
			{sideZPK, sideAPK, sideZCode, sideACode},
		} {
			samples, err := telClient.GetDeviceLatencySamples(ctx, dir.origin, dir.target, linkPK, targetEpoch)
			if err != nil {
				// Account likely doesn't exist for this epoch
				continue
			}

			samplesFound++
			sampleCount := len(samples.Samples)

			if sampleCount == 0 {
				fmt.Printf("  %s -> %s (%s): initialized, no samples yet\n",
					dir.oCode, dir.tCode, link.Code)
				continue
			}

			// Calculate stats
			var sum uint64
			min, max := samples.Samples[0], samples.Samples[0]
			for _, s := range samples.Samples {
				sum += uint64(s)
				if s < min {
					min = s
				}
				if s > max {
					max = s
				}
			}
			avgUs := float64(sum) / float64(sampleCount)
			avgMs := avgUs / 1000.0
			minMs := float64(min) / 1000.0
			maxMs := float64(max) / 1000.0

			fmt.Printf("  %s -> %s (%s): %d samples, avg %.2fms, min %.2fms, max %.2fms\n",
				dir.oCode, dir.tCode, link.Code, sampleCount, avgMs, minMs, maxMs)
		}
	}

	if samplesFound == 0 {
		fmt.Printf("  No samples found for epoch %d. Try a different epoch with --epoch flag.\n", targetEpoch)
	}

	fmt.Println()
	fmt.Println("Done.")
}
