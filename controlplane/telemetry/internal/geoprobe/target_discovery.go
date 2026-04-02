package geoprobe

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync/atomic"

	"github.com/gagliardetto/solana-go"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
)

// GeolocationUserClient fetches all GeolocationUser accounts from the onchain program.
type GeolocationUserClient interface {
	GetGeolocationUsers(ctx context.Context) ([]geolocation.KeyedGeolocationUser, error)
}

// TargetUpdate contains outbound probe targets discovered from onchain data.
type TargetUpdate struct {
	Targets []ProbeAddress
}

// InboundKeyUpdate contains inbound allowed pubkeys discovered from onchain data.
type InboundKeyUpdate struct {
	Keys [][32]byte
}

// ICMPTargetUpdate contains outbound ICMP probe targets discovered from onchain data.
type ICMPTargetUpdate struct {
	Targets []ProbeAddress
}

// targetDiscoveryFullRefreshEvery controls how often a full GeolocationUser scan
// is forced regardless of whether the GeoProbe target_update_count has changed.
// At the default 60s interval, 5 means a full refresh every ~5 minutes.
const targetDiscoveryFullRefreshEvery = 5

// TargetDiscoveryConfig holds configuration for target discovery.
type TargetDiscoveryConfig struct {
	GeoProbePubkey         solana.PublicKey
	Client                 GeolocationUserClient
	CLITargets             []ProbeAddress
	CLIIcmpTargets         []ProbeAddress
	CLIAllowedKeys         [][32]byte
	Logger                 *slog.Logger
	ProbeTargetUpdateCount *atomic.Uint32 // shared counter from parent discovery
}

// TargetDiscovery polls GeolocationUser accounts and sends target/key updates
// when changes are detected. It filters for activated, paid users whose targets
// reference this probe's pubkey.
type TargetDiscovery struct {
	log                    *slog.Logger
	geoProbePubkey         solana.PublicKey
	client                 GeolocationUserClient
	cliTargets             []ProbeAddress
	cliIcmpTargets         []ProbeAddress
	cliAllowedKeys         [][32]byte
	probeTargetUpdateCount *atomic.Uint32

	cachedTargets             []ProbeAddress
	cachedIcmpTargets         []ProbeAddress
	cachedInboundKeys         [][32]byte
	tickCount                 uint64
	lastSeenTargetUpdateCount uint32
}

// NewTargetDiscovery creates a new TargetDiscovery instance.
func NewTargetDiscovery(cfg *TargetDiscoveryConfig) (*TargetDiscovery, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if cfg.Client == nil {
		return nil, fmt.Errorf("geolocation user client is required")
	}
	if cfg.GeoProbePubkey.IsZero() {
		return nil, fmt.Errorf("geoprobe pubkey is required")
	}
	return &TargetDiscovery{
		log:                    cfg.Logger,
		geoProbePubkey:         cfg.GeoProbePubkey,
		client:                 cfg.Client,
		cliTargets:             cfg.CLITargets,
		cliIcmpTargets:         cfg.CLIIcmpTargets,
		cliAllowedKeys:         cfg.CLIAllowedKeys,
		probeTargetUpdateCount: cfg.ProbeTargetUpdateCount,
	}, nil
}

// Tick performs a single target discovery cycle and sends updates to the channels.
func (d *TargetDiscovery) Tick(ctx context.Context, targetCh chan<- TargetUpdate, keyCh chan<- InboundKeyUpdate, icmpTargetCh chan<- ICMPTargetUpdate) {
	d.discoverAndSend(ctx, targetCh, keyCh, icmpTargetCh)
}

func (d *TargetDiscovery) discoverAndSend(ctx context.Context, targetCh chan<- TargetUpdate, keyCh chan<- InboundKeyUpdate, icmpTargetCh chan<- ICMPTargetUpdate) {
	targets, icmpTargets, inboundKeys, err := d.discover(ctx)
	if err != nil {
		d.log.Warn("Target discovery tick failed", "error", err)
		return
	}

	// nil targets means the scan was skipped (target_update_count unchanged).
	if targets == nil && inboundKeys == nil {
		return
	}

	if !probeAddressSlicesEqual(targets, d.cachedTargets) {
		d.cachedTargets = targets
		select {
		case targetCh <- TargetUpdate{Targets: targets}:
		default:
			d.log.Warn("Target update channel full, skipping update")
		}
	}

	if !keySlicesEqual(inboundKeys, d.cachedInboundKeys) {
		d.cachedInboundKeys = inboundKeys
		select {
		case keyCh <- InboundKeyUpdate{Keys: inboundKeys}:
		default:
			d.log.Warn("Inbound key update channel full, skipping update")
		}
	}

	if !probeAddressSlicesEqual(icmpTargets, d.cachedIcmpTargets) {
		d.cachedIcmpTargets = icmpTargets
		select {
		case icmpTargetCh <- ICMPTargetUpdate{Targets: icmpTargets}:
		default:
			d.log.Warn("ICMP target update channel full, skipping update")
		}
	}
}

// discover performs a single discovery cycle: fetch users, filter, extract targets/keys,
// merge with CLI values. Returns nil, nil, nil, nil when the scan is skipped.
func (d *TargetDiscovery) discover(ctx context.Context) ([]ProbeAddress, []ProbeAddress, [][32]byte, error) {
	forceFullRefresh := d.tickCount%targetDiscoveryFullRefreshEvery == 0
	d.tickCount++

	if d.probeTargetUpdateCount != nil && !forceFullRefresh {
		current := d.probeTargetUpdateCount.Load()
		if current == d.lastSeenTargetUpdateCount && d.tickCount > 1 {
			d.log.Debug("GeoProbe target_update_count unchanged, skipping target scan",
				"targetUpdateCount", current)
			return nil, nil, nil, nil
		}
		d.lastSeenTargetUpdateCount = current
	}

	users, err := d.client.GetGeolocationUsers(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to fetch GeolocationUser accounts: %w", err)
	}

	var probePKBytes [32]byte
	copy(probePKBytes[:], d.geoProbePubkey[:])

	var onchainTargets []ProbeAddress
	var onchainIcmpTargets []ProbeAddress
	var onchainKeys [][32]byte
	seenKeys := make(map[[32]byte]struct{})

	for i := range users {
		user := &users[i].GeolocationUser

		// Security invariant: only activated AND paid users are eligible.
		if user.Status != geolocation.GeolocationUserStatusActivated {
			continue
		}
		if user.PaymentStatus != geolocation.GeolocationPaymentStatusPaid {
			continue
		}

		for j := range user.Targets {
			target := &user.Targets[j]

			// Only process targets assigned to this probe.
			if target.GeoProbePK != d.geoProbePubkey {
				continue
			}

			switch target.TargetType {
			case geolocation.GeoLocationTargetTypeOutbound:
				addr := targetToProbeAddress(target)
				if err := addr.Validate(); err != nil {
					d.log.Warn("Skipping invalid outbound target",
						"user", users[i].Code, "addr", addr, "error", err)
					continue
				}
				if err := addr.ValidateScope(); err != nil {
					d.log.Warn("Rejecting non-public outbound target",
						"user", users[i].Code, "addr", addr, "error", err)
					continue
				}
				onchainTargets = append(onchainTargets, addr)

			case geolocation.GeoLocationTargetTypeInbound:
				var key [32]byte
				copy(key[:], target.TargetPK[:])
				if _, exists := seenKeys[key]; !exists {
					seenKeys[key] = struct{}{}
					onchainKeys = append(onchainKeys, key)
				}

			case geolocation.GeoLocationTargetTypeOutboundIcmp:
				addr := icmpTargetToProbeAddress(target)
				if err := addr.ValidateICMP(); err != nil {
					d.log.Warn("Skipping invalid outbound ICMP target",
						"user", users[i].Code, "addr", addr, "error", err)
					continue
				}
				if err := addr.ValidateScope(); err != nil {
					d.log.Warn("Rejecting non-public outbound ICMP target",
						"user", users[i].Code, "addr", addr, "error", err)
					continue
				}
				onchainIcmpTargets = append(onchainIcmpTargets, addr)
			}
		}
	}

	mergedTargets := mergeProbes(d.cliTargets, onchainTargets)
	mergedIcmpTargets := mergeProbes(d.cliIcmpTargets, onchainIcmpTargets)
	mergedKeys := mergeKeys(d.cliAllowedKeys, onchainKeys)

	// Sync lastSeenTargetUpdateCount after a full scan (covers forced refresh path).
	if d.probeTargetUpdateCount != nil {
		d.lastSeenTargetUpdateCount = d.probeTargetUpdateCount.Load()
	}

	d.log.Debug("Target discovery tick",
		"users", len(users),
		"onchainOutbound", len(onchainTargets),
		"onchainOutboundIcmp", len(onchainIcmpTargets),
		"onchainInbound", len(onchainKeys),
		"cliTargets", len(d.cliTargets),
		"cliKeys", len(d.cliAllowedKeys),
		"mergedTargets", len(mergedTargets),
		"mergedIcmpTargets", len(mergedIcmpTargets),
		"mergedKeys", len(mergedKeys),
	)

	return mergedTargets, mergedIcmpTargets, mergedKeys, nil
}

// targetToProbeAddress converts a GeolocationTarget to a ProbeAddress.
func targetToProbeAddress(t *geolocation.GeolocationTarget) ProbeAddress {
	host := fmt.Sprintf("%d.%d.%d.%d",
		t.IPAddress[0], t.IPAddress[1], t.IPAddress[2], t.IPAddress[3])
	return ProbeAddress{
		Host:      host,
		Port:      t.LocationOffsetPort,
		TWAMPPort: telemetryconfig.DefaultGeoprobeTWAMPPort,
	}
}

// icmpTargetToProbeAddress converts an OutboundIcmp GeolocationTarget to a ProbeAddress.
func icmpTargetToProbeAddress(t *geolocation.GeolocationTarget) ProbeAddress {
	host := fmt.Sprintf("%d.%d.%d.%d",
		t.IPAddress[0], t.IPAddress[1], t.IPAddress[2], t.IPAddress[3])
	return ProbeAddress{
		Host:      host,
		Port:      t.LocationOffsetPort,
		TWAMPPort: 0,
	}
}

// mergeKeys combines two key slices, deduplicating by value.
func mergeKeys(a, b [][32]byte) [][32]byte {
	seen := make(map[[32]byte]struct{}, len(a)+len(b))
	merged := make([][32]byte, 0, len(a)+len(b))
	for _, k := range a {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			merged = append(merged, k)
		}
	}
	for _, k := range b {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			merged = append(merged, k)
		}
	}
	return merged
}

// probeAddressSlicesEqual checks if two ProbeAddress slices are equal by content.
// Slices are sorted by string representation before comparison.
func probeAddressSlicesEqual(a, b []ProbeAddress) bool {
	if len(a) != len(b) {
		return false
	}
	aSorted := make([]string, len(a))
	bSorted := make([]string, len(b))
	for i := range a {
		aSorted[i] = a[i].String()
	}
	for i := range b {
		bSorted[i] = b[i].String()
	}
	sort.Strings(aSorted)
	sort.Strings(bSorted)
	for i := range aSorted {
		if aSorted[i] != bSorted[i] {
			return false
		}
	}
	return true
}

// keySlicesEqual checks if two key slices contain the same keys (order-independent).
func keySlicesEqual(a, b [][32]byte) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[[32]byte]struct{}, len(a))
	for _, k := range a {
		set[k] = struct{}{}
	}
	for _, k := range b {
		if _, ok := set[k]; !ok {
			return false
		}
	}
	return true
}
