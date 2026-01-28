// Package main implements an RFC11 resource migration script that generates shell commands
// to migrate existing resource allocations to on-chain ResourceExtension accounts.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

type cliConfig struct {
	network   string
	rpcURL    string
	programID string
	output    string
	verbose   bool
	parallel  int
}

// networkConfig holds RPC URL and program ID for a network
type networkConfig struct {
	rpcURL    string
	programID string
}

// Known network configurations from config/constants.go
var networks = map[string]networkConfig{
	"mainnet-beta": {
		rpcURL:    config.MainnetLedgerPublicRPCURL,
		programID: config.MainnetServiceabilityProgramID,
	},
	"testnet": {
		rpcURL:    config.TestnetLedgerPublicRPCURL,
		programID: config.TestnetServiceabilityProgramID,
	},
	"devnet": {
		rpcURL:    config.DevnetLedgerPublicRPCURL,
		programID: config.DevnetServiceabilityProgramID,
	},
	"localnet": {
		rpcURL:    config.LocalnetLedgerPublicRPCURL,
		programID: config.LocalnetServiceabilityProgramID,
	},
}

func run() error {
	cfg := cliConfig{}

	flag.StringVar(&cfg.network, "network", "mainnet-beta", "Network: mainnet-beta, testnet, devnet, localnet")
	flag.StringVar(&cfg.rpcURL, "rpc", "", "Override RPC URL (optional)")
	flag.StringVar(&cfg.programID, "program-id", "", "Override program ID (optional)")
	flag.StringVar(&cfg.output, "output", "", "Output file for migration script (default: dry-run to stdout)")
	flag.BoolVar(&cfg.verbose, "verbose", false, "Show detailed information during dry-run")
	flag.IntVar(&cfg.parallel, "parallel", 8, "Number of parallel jobs for migration script (0 for sequential)")
	help := flag.Bool("help", false, "Show help")

	flag.Parse()

	if *help {
		printUsage()
		return nil
	}

	// Get network config
	netCfg, ok := networks[cfg.network]
	if !ok {
		return fmt.Errorf("unknown network %q, must be one of: mainnet-beta, testnet, devnet, localnet", cfg.network)
	}

	// Apply overrides if provided
	rpcURL := netCfg.rpcURL
	if cfg.rpcURL != "" {
		rpcURL = cfg.rpcURL
	}
	programIDStr := netCfg.programID
	if cfg.programID != "" {
		programIDStr = cfg.programID
	}

	programID, err := solana.PublicKeyFromBase58(programIDStr)
	if err != nil {
		return fmt.Errorf("invalid program ID: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create RPC client and fetch program data
	rpcClient := rpc.New(rpcURL)
	client := serviceability.New(rpcClient, programID)

	fmt.Fprintf(os.Stderr, "Network: %s\n", cfg.network)
	fmt.Fprintf(os.Stderr, "RPC: %s\n", rpcURL)
	fmt.Fprintf(os.Stderr, "Program ID: %s\n", programIDStr)
	fmt.Fprintf(os.Stderr, "Fetching program data...\n")
	data, err := client.GetProgramData(ctx)
	if err != nil {
		return fmt.Errorf("failed to get program data: %w", err)
	}

	// Generate migration
	migration := generateMigration(data, cfg.verbose)

	if cfg.output == "" {
		// Dry-run mode: print summary to stdout
		printDryRun(os.Stdout, cfg, migration)
	} else {
		// Script mode: write script to file
		f, err := os.Create(cfg.output)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer f.Close()

		writeScript(f, migration, cfg.parallel)
		fmt.Fprintf(os.Stderr, "Migration script written to %s\n", cfg.output)
		if cfg.parallel > 0 {
			fmt.Fprintf(os.Stderr, "Parallelism: %d jobs (requires GNU parallel)\n", cfg.parallel)
		}
	}

	return nil
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `RFC11 Resource Migration Script

This script generates shell commands to migrate existing resource allocations
to on-chain ResourceExtension accounts.

Usage:
  rfc11-resource-migration [options]

Options:
  --network <name>    Network: mainnet-beta (default), testnet, devnet, localnet
  --rpc <URL>         Override RPC URL (optional)
  --program-id <ID>   Override program ID (optional)
  --output <file>     Output file for migration script (default: dry-run to stdout)
  --verbose           Show detailed information during dry-run
  --parallel <n>      Number of parallel jobs (default: 8, 0 for sequential)
  --help              Show this help message

Examples:
  # Dry-run on mainnet (default)
  rfc11-resource-migration

  # Dry-run on devnet
  rfc11-resource-migration --network devnet

  # Generate parallel migration script (default: 8 jobs)
  rfc11-resource-migration --output migration.sh

  # Generate script with custom parallelism
  rfc11-resource-migration --output migration.sh --parallel 16

  # Generate sequential script (no parallel)
  rfc11-resource-migration --output migration.sh --parallel 0

  # Verbose dry-run with details
  rfc11-resource-migration --verbose

  # Override RPC URL
  rfc11-resource-migration --network mainnet-beta --rpc https://custom-rpc.example.com
`)
}

// migration holds all the commands to execute
type migration struct {
	globalCreates     []createCommand
	perDeviceCreates  []createCommand
	deviceAllocations []allocateCommand // Device loopback IPs
	linkAllocations   []allocateCommand
	userAllocations   []allocateCommand
	mcgroupAllocs     []allocateCommand
	deviceInfo        map[string]deviceInfo // pubkey -> deviceInfo for display
}

type deviceInfo struct {
	code       string
	dzPrefixes []string
}

type createCommand struct {
	resourceType string
	associatedPK string // optional, for per-device resources
	index        int    // optional, for per-device resources
	comment      string // human-readable comment
}

type allocateCommand struct {
	resourceType string
	associatedPK string // optional, for per-device resources
	index        int    // optional, for per-device resources
	allocation   string // the IP or ID to allocate
	comment      string // human-readable comment
}

func generateMigration(data *serviceability.ProgramData, verbose bool) *migration {
	m := &migration{
		deviceInfo: make(map[string]deviceInfo),
	}

	// Collect device info for display
	activatedDevices := []serviceability.Device{}
	for _, d := range data.Devices {
		if d.Status == serviceability.DeviceStatusActivated {
			activatedDevices = append(activatedDevices, d)
			pk := base58.Encode(d.PubKey[:])
			prefixes := make([]string, len(d.DzPrefixes))
			for i, p := range d.DzPrefixes {
				prefixes[i] = onChainNetToString(p)
			}
			m.deviceInfo[pk] = deviceInfo{
				code:       d.Code,
				dzPrefixes: prefixes,
			}
		}
	}

	// Sort devices by code for consistent output
	sort.Slice(activatedDevices, func(i, j int) bool {
		return activatedDevices[i].Code < activatedDevices[j].Code
	})

	// 1. Global ResourceExtension accounts
	m.globalCreates = []createCommand{
		{resourceType: "device-tunnel-block", comment: "Global DeviceTunnelBlock for link tunnel_net (/31)"},
		{resourceType: "user-tunnel-block", comment: "Global UserTunnelBlock for user tunnel_net (/31)"},
		{resourceType: "multicast-group-block", comment: "Global MulticastGroupBlock for multicast_ip (/32)"},
		{resourceType: "link-ids", comment: "Global LinkIds for link tunnel_id (u16)"},
		{resourceType: "segment-routing-ids", comment: "Global SegmentRoutingIds (reserved for future use)"},
	}

	// 2. Per-device ResourceExtension accounts
	for _, d := range activatedDevices {
		pk := base58.Encode(d.PubKey[:])
		m.perDeviceCreates = append(m.perDeviceCreates, createCommand{
			resourceType: "tunnel-ids",
			associatedPK: pk,
			index:        0,
			comment:      fmt.Sprintf("TunnelIds for device %s", d.Code),
		})
		// Create DzPrefixBlock for each dz_prefix
		for i, prefix := range d.DzPrefixes {
			m.perDeviceCreates = append(m.perDeviceCreates, createCommand{
				resourceType: "dz-prefix-block",
				associatedPK: pk,
				index:        i,
				comment:      fmt.Sprintf("DzPrefixBlock for device %s index %d (%s)", d.Code, i, onChainNetToString(prefix)),
			})
		}
	}

	// 3. Device loopback interface allocations (loopback IPs -> DzPrefixBlock)
	for _, d := range activatedDevices {
		pk := base58.Encode(d.PubKey[:])
		for _, iface := range d.Interfaces {
			// Only process loopback interfaces with allocated IPs
			if iface.LoopbackType == serviceability.LoopbackTypeNone {
				continue
			}
			loopbackIP := onChainNetToIP(iface.IpNet)
			if loopbackIP == "" || loopbackIP == "0.0.0.0" {
				continue
			}
			// Find which DzPrefixBlock index this IP belongs to
			prefixIndex := findDzPrefixIndex(d.DzPrefixes, [4]uint8{iface.IpNet[0], iface.IpNet[1], iface.IpNet[2], iface.IpNet[3]})
			m.deviceAllocations = append(m.deviceAllocations, allocateCommand{
				resourceType: "device-tunnel-block",
				associatedPK: pk,
				index:        prefixIndex,
				allocation:   loopbackIP,
				comment:      fmt.Sprintf("Device %s loopback %s (%s) ip=%s", d.Code, iface.Name, iface.LoopbackType, loopbackIP),
			})
		}
	}

	// 4. Link allocations (tunnel_net -> DeviceTunnelBlock, tunnel_id -> LinkIds)
	// Note: Renumbered from 3 after adding device loopback allocations
	// Sort links by code for consistent output
	links := make([]serviceability.Link, len(data.Links))
	copy(links, data.Links)
	sort.Slice(links, func(i, j int) bool {
		return links[i].Code < links[j].Code
	})

	for _, link := range links {
		// Only process activated links with allocations
		if link.Status != serviceability.LinkStatusActivated {
			continue
		}

		tunnelNet := onChainNetToString(link.TunnelNet)
		if tunnelNet != "" {
			m.linkAllocations = append(m.linkAllocations, allocateCommand{
				resourceType: "device-tunnel-block",
				allocation:   tunnelNet,
				comment:      fmt.Sprintf("Link %s tunnel_net=%s", link.Code, tunnelNet),
			})
		}

		m.linkAllocations = append(m.linkAllocations, allocateCommand{
			resourceType: "link-ids",
			allocation:   fmt.Sprintf("%d", link.TunnelId),
			comment:      fmt.Sprintf("Link %s tunnel_id=%d", link.Code, link.TunnelId),
		})
	}

	// 5. User allocations (tunnel_net -> UserTunnelBlock, tunnel_id -> TunnelIds(device), dz_ip -> DzPrefixBlock(device))
	// Create a map of device pubkey -> code for user comments
	deviceCodeMap := make(map[string]string)
	deviceDzPrefixMap := make(map[string][][5]uint8)
	for _, d := range data.Devices {
		pk := base58.Encode(d.PubKey[:])
		deviceCodeMap[pk] = d.Code
		deviceDzPrefixMap[pk] = d.DzPrefixes
	}

	// Sort users by client IP for consistent output
	users := make([]serviceability.User, len(data.Users))
	copy(users, data.Users)
	sort.Slice(users, func(i, j int) bool {
		return net.IP(users[i].ClientIp[:]).String() < net.IP(users[j].ClientIp[:]).String()
	})

	for _, user := range users {
		// Only process activated users
		if user.Status != serviceability.UserStatusActivated {
			continue
		}

		clientIP := net.IP(user.ClientIp[:]).String()
		devicePK := base58.Encode(user.DevicePubKey[:])
		deviceCode := deviceCodeMap[devicePK]
		if deviceCode == "" {
			deviceCode = "unknown"
		}

		// tunnel_net -> UserTunnelBlock
		tunnelNet := onChainNetToString(user.TunnelNet)
		if tunnelNet != "" {
			m.userAllocations = append(m.userAllocations, allocateCommand{
				resourceType: "user-tunnel-block",
				allocation:   tunnelNet,
				comment:      fmt.Sprintf("User %s on %s tunnel_net=%s", clientIP, deviceCode, tunnelNet),
			})
		}

		// tunnel_id -> TunnelIds(device, 0)
		m.userAllocations = append(m.userAllocations, allocateCommand{
			resourceType: "tunnel-ids",
			associatedPK: devicePK,
			index:        0,
			allocation:   fmt.Sprintf("%d", user.TunnelId),
			comment:      fmt.Sprintf("User %s on %s tunnel_id=%d", clientIP, deviceCode, user.TunnelId),
		})

		// dz_ip -> DzPrefixBlock(device, index)
		// Skip IBRL users - they use their client IP directly, not an allocated DZ IP
		dzIP := net.IP(user.DzIp[:]).String()
		isIBRL := user.UserType == serviceability.UserTypeIBRL
		if dzIP != "0.0.0.0" && !isIBRL {
			// Find which DzPrefixBlock index this IP belongs to
			prefixIndex := findDzPrefixIndex(deviceDzPrefixMap[devicePK], user.DzIp)
			m.userAllocations = append(m.userAllocations, allocateCommand{
				resourceType: "dz-prefix-block",
				associatedPK: devicePK,
				index:        prefixIndex,
				allocation:   dzIP,
				comment:      fmt.Sprintf("User %s on %s dz_ip=%s, ut=%s", clientIP, deviceCode, dzIP, user.UserType),
			})
		}
	}

	// 6. MulticastGroup allocations (multicast_ip -> MulticastGroupBlock)
	// Sort multicast groups by code for consistent output
	mcgroups := make([]serviceability.MulticastGroup, len(data.MulticastGroups))
	copy(mcgroups, data.MulticastGroups)
	sort.Slice(mcgroups, func(i, j int) bool {
		return mcgroups[i].Code < mcgroups[j].Code
	})

	for _, mcg := range mcgroups {
		// Only process activated multicast groups
		if mcg.Status != serviceability.MulticastGroupStatusActivated {
			continue
		}

		multicastIP := net.IP(mcg.MulticastIp[:]).String()
		if multicastIP != "0.0.0.0" {
			m.mcgroupAllocs = append(m.mcgroupAllocs, allocateCommand{
				resourceType: "multicast-group-block",
				allocation:   multicastIP,
				comment:      fmt.Sprintf("MulticastGroup %s multicast_ip=%s", mcg.Code, multicastIP),
			})
		}
	}

	return m
}

// findDzPrefixIndex finds which dz_prefix index an IP belongs to
func findDzPrefixIndex(prefixes [][5]uint8, ip [4]uint8) int {
	for i, prefix := range prefixes {
		// Check if IP is in this prefix's network
		prefixLen := prefix[4]
		if prefixLen == 0 {
			continue
		}
		if ipInPrefix(ip, prefix) {
			return i
		}
	}
	return 0 // default to first prefix
}

// ipInPrefix checks if an IP is within a prefix
func ipInPrefix(ip [4]uint8, prefix [5]uint8) bool {
	prefixLen := int(prefix[4])
	if prefixLen == 0 {
		return false
	}

	// Convert to uint32 for comparison
	ipVal := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	prefixVal := uint32(prefix[0])<<24 | uint32(prefix[1])<<16 | uint32(prefix[2])<<8 | uint32(prefix[3])

	// Create mask
	mask := uint32(0xFFFFFFFF) << (32 - prefixLen)

	return (ipVal & mask) == (prefixVal & mask)
}

func printDryRun(w io.Writer, cfg cliConfig, m *migration) {
	fmt.Fprintf(w, "\n=== RFC11 Resource Migration Dry-Run ===\n\n")

	fmt.Fprintf(w, "--- Global ResourceExtension Accounts ---\n")
	for _, c := range m.globalCreates {
		fmt.Fprintf(w, "[CREATE] %s\n", c.resourceType)
		if cfg.verbose {
			fmt.Fprintf(w, "         %s\n", c.comment)
		}
	}

	fmt.Fprintf(w, "\n--- Per-Device ResourceExtension Accounts ---\n")
	currentDevice := ""
	for _, c := range m.perDeviceCreates {
		if c.associatedPK != currentDevice {
			currentDevice = c.associatedPK
			info := m.deviceInfo[currentDevice]
			fmt.Fprintf(w, "Device: %s (%s...)\n", info.code, c.associatedPK[:12])
		}
		fmt.Fprintf(w, "  [CREATE] %s index=%d\n", c.resourceType, c.index)
	}

	fmt.Fprintf(w, "\n--- Device Loopback Allocations (%d interfaces) ---\n", len(m.deviceAllocations))
	for _, a := range m.deviceAllocations {
		info := m.deviceInfo[a.associatedPK]
		fmt.Fprintf(w, "[ALLOCATE] %s(%s, %d): %s\n", a.resourceType, info.code, a.index, a.allocation)
		if cfg.verbose {
			fmt.Fprintf(w, "           %s\n", a.comment)
		}
	}

	fmt.Fprintf(w, "\n--- Link Allocations (%d links) ---\n", len(m.linkAllocations)/2)
	for _, a := range m.linkAllocations {
		fmt.Fprintf(w, "[ALLOCATE] %s: %s\n", a.resourceType, a.allocation)
		if cfg.verbose {
			fmt.Fprintf(w, "           %s\n", a.comment)
		}
	}

	fmt.Fprintf(w, "\n--- User Allocations (%d users) ---\n", len(m.userAllocations)/3)
	for _, a := range m.userAllocations {
		if a.associatedPK != "" {
			info := m.deviceInfo[a.associatedPK]
			fmt.Fprintf(w, "[ALLOCATE] %s(%s, %d): %s\n", a.resourceType, info.code, a.index, a.allocation)
		} else {
			fmt.Fprintf(w, "[ALLOCATE] %s: %s\n", a.resourceType, a.allocation)
		}
		if cfg.verbose {
			fmt.Fprintf(w, "           %s\n", a.comment)
		}
	}

	fmt.Fprintf(w, "\n--- MulticastGroup Allocations (%d groups) ---\n", len(m.mcgroupAllocs))
	for _, a := range m.mcgroupAllocs {
		fmt.Fprintf(w, "[ALLOCATE] %s: %s\n", a.resourceType, a.allocation)
		if cfg.verbose {
			fmt.Fprintf(w, "           %s\n", a.comment)
		}
	}

	// Summary
	totalCreates := len(m.globalCreates) + len(m.perDeviceCreates)
	totalAllocs := len(m.deviceAllocations) + len(m.linkAllocations) + len(m.userAllocations) + len(m.mcgroupAllocs)

	fmt.Fprintf(w, "\n=== Summary ===\n")
	fmt.Fprintf(w, "ResourceExtension accounts to create: %d\n", totalCreates)
	fmt.Fprintf(w, "Resources to allocate: %d\n", totalAllocs)
}

func writeScript(w io.Writer, m *migration, parallelism int) {
	fmt.Fprintf(w, "#!/bin/bash\n")
	fmt.Fprintf(w, "set -euo pipefail\n\n")
	fmt.Fprintf(w, "# ============================================\n")
	fmt.Fprintf(w, "# RFC11 Resource Migration Script\n")
	fmt.Fprintf(w, "# Generated: %s\n", time.Now().Format(time.RFC3339))
	if parallelism > 0 {
		fmt.Fprintf(w, "# Parallelism: %d jobs\n", parallelism)
	} else {
		fmt.Fprintf(w, "# Mode: Sequential\n")
	}
	fmt.Fprintf(w, "# ============================================\n\n")

	// Check for GNU parallel if parallel mode
	if parallelism > 0 {
		fmt.Fprintf(w, "# Check for GNU parallel\n")
		fmt.Fprintf(w, "if ! command -v parallel &> /dev/null; then\n")
		fmt.Fprintf(w, "    echo \"ERROR: GNU parallel is required but not installed.\"\n")
		fmt.Fprintf(w, "    echo \"Install with: brew install parallel (macOS) or apt install parallel (Linux)\"\n")
		fmt.Fprintf(w, "    exit 1\n")
		fmt.Fprintf(w, "fi\n\n")
	}

	// Track failed commands for retry
	fmt.Fprintf(w, "# Track failures\n")
	fmt.Fprintf(w, "FAILED_COMMANDS=\"\"\n")
	fmt.Fprintf(w, "TOTAL_COMMANDS=0\n")
	fmt.Fprintf(w, "SUCCESSFUL_COMMANDS=0\n\n")

	// Helper function for running commands with error tracking
	fmt.Fprintf(w, "# Run command and track result\n")
	fmt.Fprintf(w, "run_cmd() {\n")
	fmt.Fprintf(w, "    if eval \"$1\"; then\n")
	fmt.Fprintf(w, "        ((SUCCESSFUL_COMMANDS++)) || true\n")
	fmt.Fprintf(w, "    else\n")
	fmt.Fprintf(w, "        echo \"FAILED: $1\"\n")
	fmt.Fprintf(w, "        FAILED_COMMANDS=\"$FAILED_COMMANDS$1\\n\"\n")
	fmt.Fprintf(w, "    fi\n")
	fmt.Fprintf(w, "    ((TOTAL_COMMANDS++)) || true\n")
	fmt.Fprintf(w, "}\n\n")

	// Phase 1: Global creates (sequential - must happen first)
	fmt.Fprintf(w, "# ============================================\n")
	fmt.Fprintf(w, "# Phase 1: Create Global ResourceExtension Accounts\n")
	fmt.Fprintf(w, "# These must complete before any allocations can happen\n")
	fmt.Fprintf(w, "# ============================================\n")
	fmt.Fprintf(w, "echo \"[Phase 1/%d] Creating Global ResourceExtension Accounts (%d commands)...\"\n", numPhases(m), len(m.globalCreates))
	for _, c := range m.globalCreates {
		fmt.Fprintf(w, "# %s\n", c.comment)
		fmt.Fprintf(w, "run_cmd 'doublezero resource create --resource-type %s'\n", c.resourceType)
	}
	fmt.Fprintf(w, "\n")

	// Phase 2: Per-device creates (parallel - can run concurrently)
	fmt.Fprintf(w, "# ============================================\n")
	fmt.Fprintf(w, "# Phase 2: Create Per-Device ResourceExtension Accounts\n")
	fmt.Fprintf(w, "# These can run in parallel as they don't depend on each other\n")
	fmt.Fprintf(w, "# ============================================\n")
	fmt.Fprintf(w, "echo \"[Phase 2/%d] Creating Per-Device ResourceExtension Accounts (%d commands)...\"\n", numPhases(m), len(m.perDeviceCreates))

	if parallelism > 0 && len(m.perDeviceCreates) > 0 {
		writeParallelCreates(w, m.perDeviceCreates, parallelism)
	} else {
		for _, c := range m.perDeviceCreates {
			fmt.Fprintf(w, "# %s\n", c.comment)
			fmt.Fprintf(w, "run_cmd 'doublezero resource create --resource-type %s --associated-pubkey %s --index %d'\n",
				c.resourceType, c.associatedPK, c.index)
		}
	}
	fmt.Fprintf(w, "\n")

	// Phase 3: Device loopback allocations (parallel)
	if len(m.deviceAllocations) > 0 {
		fmt.Fprintf(w, "# ============================================\n")
		fmt.Fprintf(w, "# Phase 3: Allocate Device Loopback IPs\n")
		fmt.Fprintf(w, "# ============================================\n")
		fmt.Fprintf(w, "echo \"[Phase 3/%d] Allocating Device Loopback IPs (%d commands)...\"\n", numPhases(m), len(m.deviceAllocations))

		if parallelism > 0 {
			writeParallelAllocates(w, m.deviceAllocations, parallelism)
		} else {
			for _, a := range m.deviceAllocations {
				fmt.Fprintf(w, "# %s\n", a.comment)
				fmt.Fprintf(w, "run_cmd 'doublezero resource allocate --resource-type %s --associated-pubkey %s --index %d --requested-allocation %s'\n",
					a.resourceType, a.associatedPK, a.index, a.allocation)
			}
		}
		fmt.Fprintf(w, "\n")
	}

	// Phase 4: Link allocations (parallel)
	fmt.Fprintf(w, "# ============================================\n")
	fmt.Fprintf(w, "# Phase 4: Allocate Link Resources\n")
	fmt.Fprintf(w, "# ============================================\n")
	fmt.Fprintf(w, "echo \"[Phase 4/%d] Allocating Link Resources (%d commands)...\"\n", numPhases(m), len(m.linkAllocations))

	if parallelism > 0 && len(m.linkAllocations) > 0 {
		writeParallelAllocates(w, m.linkAllocations, parallelism)
	} else {
		for _, a := range m.linkAllocations {
			fmt.Fprintf(w, "# %s\n", a.comment)
			fmt.Fprintf(w, "run_cmd 'doublezero resource allocate --resource-type %s --requested-allocation %s'\n",
				a.resourceType, a.allocation)
		}
	}
	fmt.Fprintf(w, "\n")

	// Phase 5: User allocations (parallel - largest phase)
	fmt.Fprintf(w, "# ============================================\n")
	fmt.Fprintf(w, "# Phase 5: Allocate User Resources\n")
	fmt.Fprintf(w, "# This is the largest phase - parallelism helps significantly here\n")
	fmt.Fprintf(w, "# ============================================\n")
	fmt.Fprintf(w, "echo \"[Phase 5/%d] Allocating User Resources (%d commands)...\"\n", numPhases(m), len(m.userAllocations))

	if parallelism > 0 && len(m.userAllocations) > 0 {
		writeParallelAllocates(w, m.userAllocations, parallelism)
	} else {
		for _, a := range m.userAllocations {
			fmt.Fprintf(w, "# %s\n", a.comment)
			if a.associatedPK != "" {
				fmt.Fprintf(w, "run_cmd 'doublezero resource allocate --resource-type %s --associated-pubkey %s --index %d --requested-allocation %s'\n",
					a.resourceType, a.associatedPK, a.index, a.allocation)
			} else {
				fmt.Fprintf(w, "run_cmd 'doublezero resource allocate --resource-type %s --requested-allocation %s'\n",
					a.resourceType, a.allocation)
			}
		}
	}
	fmt.Fprintf(w, "\n")

	// Phase 6: Multicast group allocations (parallel)
	if len(m.mcgroupAllocs) > 0 {
		fmt.Fprintf(w, "# ============================================\n")
		fmt.Fprintf(w, "# Phase 6: Allocate MulticastGroup Resources\n")
		fmt.Fprintf(w, "# ============================================\n")
		fmt.Fprintf(w, "echo \"[Phase 6/%d] Allocating MulticastGroup Resources (%d commands)...\"\n", numPhases(m), len(m.mcgroupAllocs))

		if parallelism > 0 {
			writeParallelAllocates(w, m.mcgroupAllocs, parallelism)
		} else {
			for _, a := range m.mcgroupAllocs {
				fmt.Fprintf(w, "# %s\n", a.comment)
				fmt.Fprintf(w, "run_cmd 'doublezero resource allocate --resource-type %s --requested-allocation %s'\n",
					a.resourceType, a.allocation)
			}
		}
		fmt.Fprintf(w, "\n")
	}

	// Summary
	fmt.Fprintf(w, "# ============================================\n")
	fmt.Fprintf(w, "# Migration Summary\n")
	fmt.Fprintf(w, "# ============================================\n")
	fmt.Fprintf(w, "echo \"\"\n")
	fmt.Fprintf(w, "echo \"=== Migration Complete ===\"\n")
	fmt.Fprintf(w, "echo \"Total commands: $TOTAL_COMMANDS\"\n")
	fmt.Fprintf(w, "echo \"Successful: $SUCCESSFUL_COMMANDS\"\n")
	fmt.Fprintf(w, "if [ -n \"$FAILED_COMMANDS\" ]; then\n")
	fmt.Fprintf(w, "    echo \"Failed commands:\"\n")
	fmt.Fprintf(w, "    echo -e \"$FAILED_COMMANDS\"\n")
	fmt.Fprintf(w, "    exit 1\n")
	fmt.Fprintf(w, "fi\n")
}

// numPhases returns the maximum phase number in the migration
// Phases: 1=global creates, 2=per-device creates, 3=device loopbacks (optional),
// 4=links, 5=users, 6=multicast (optional)
func numPhases(m *migration) int {
	if len(m.mcgroupAllocs) > 0 {
		return 6
	}
	return 5
}

// writeParallelCreates writes create commands for GNU parallel execution
func writeParallelCreates(w io.Writer, cmds []createCommand, parallelism int) {
	fmt.Fprintf(w, "parallel --halt soon,fail=10%% --retries 3 -j %d :::: <<'PARALLEL_EOF'\n", parallelism)
	for _, c := range cmds {
		fmt.Fprintf(w, "doublezero resource create --resource-type %s --associated-pubkey %s --index %d\n",
			c.resourceType, c.associatedPK, c.index)
	}
	fmt.Fprintf(w, "PARALLEL_EOF\n")
	fmt.Fprintf(w, "PARALLEL_EXIT=$?\n")
	fmt.Fprintf(w, "if [ $PARALLEL_EXIT -ne 0 ]; then\n")
	fmt.Fprintf(w, "    echo \"WARNING: Some parallel create commands failed (exit code: $PARALLEL_EXIT)\"\n")
	fmt.Fprintf(w, "fi\n")
}

// writeParallelAllocates writes allocate commands for GNU parallel execution
func writeParallelAllocates(w io.Writer, cmds []allocateCommand, parallelism int) {
	fmt.Fprintf(w, "parallel --halt soon,fail=10%% --retries 3 -j %d :::: <<'PARALLEL_EOF'\n", parallelism)
	for _, a := range cmds {
		if a.associatedPK != "" {
			fmt.Fprintf(w, "doublezero resource allocate --resource-type %s --associated-pubkey %s --index %d --requested-allocation %s\n",
				a.resourceType, a.associatedPK, a.index, a.allocation)
		} else {
			fmt.Fprintf(w, "doublezero resource allocate --resource-type %s --requested-allocation %s\n",
				a.resourceType, a.allocation)
		}
	}
	fmt.Fprintf(w, "PARALLEL_EOF\n")
	fmt.Fprintf(w, "PARALLEL_EXIT=$?\n")
	fmt.Fprintf(w, "if [ $PARALLEL_EXIT -ne 0 ]; then\n")
	fmt.Fprintf(w, "    echo \"WARNING: Some parallel allocate commands failed (exit code: $PARALLEL_EXIT)\"\n")
	fmt.Fprintf(w, "fi\n")
}

// onChainNetToString converts on-chain network format to string (e.g., "10.0.0.1/31")
func onChainNetToString(n [5]uint8) string {
	prefixLen := n[4]
	if prefixLen > 0 && prefixLen <= 32 {
		ip := net.IP(n[:4])
		return fmt.Sprintf("%s/%d", ip.String(), prefixLen)
	}
	return ""
}

// onChainNetToIP extracts just the IP address from on-chain network format (without CIDR)
func onChainNetToIP(n [5]uint8) string {
	ip := net.IP(n[:4])
	return ip.String()
}
