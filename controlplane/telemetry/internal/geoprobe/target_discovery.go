package geoprobe

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

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

// TargetDiscoveryConfig holds configuration for target discovery.
type TargetDiscoveryConfig struct {
	GeoProbePubkey solana.PublicKey
	Client         GeolocationUserClient
	CLITargets     []ProbeAddress
	CLIAllowedKeys [][32]byte
	Interval       time.Duration
	Logger         *slog.Logger
}

// TargetDiscovery polls GeolocationUser accounts and sends target/key updates
// when changes are detected. It filters for activated, paid users whose targets
// reference this probe's pubkey.
type TargetDiscovery struct {
	log            *slog.Logger
	geoProbePubkey solana.PublicKey
	client         GeolocationUserClient
	cliTargets     []ProbeAddress
	cliAllowedKeys [][32]byte
	interval       time.Duration

	cachedTargets     []ProbeAddress
	cachedInboundKeys [][32]byte
	tickCount         uint64
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
	if cfg.Interval <= 0 {
		return nil, fmt.Errorf("interval must be greater than 0")
	}

	return &TargetDiscovery{
		log:            cfg.Logger,
		geoProbePubkey: cfg.GeoProbePubkey,
		client:         cfg.Client,
		cliTargets:     cfg.CLITargets,
		cliAllowedKeys: cfg.CLIAllowedKeys,
		interval:       cfg.Interval,
	}, nil
}

// Run starts the discovery polling loop, sending updates to the provided channels.
// It performs an immediate discovery tick, then repeats at the configured interval.
func (d *TargetDiscovery) Run(ctx context.Context, targetCh chan<- TargetUpdate, keyCh chan<- InboundKeyUpdate) {
	d.log.Info("Starting target discovery",
		"interval", d.interval,
		"geoProbePubkey", d.geoProbePubkey,
		"cliTargets", len(d.cliTargets),
		"cliAllowedKeys", len(d.cliAllowedKeys),
	)

	d.discoverAndSend(ctx, targetCh, keyCh)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.log.Info("Target discovery shutting down")
			return
		case <-ticker.C:
			d.discoverAndSend(ctx, targetCh, keyCh)
		}
	}
}

func (d *TargetDiscovery) discoverAndSend(ctx context.Context, targetCh chan<- TargetUpdate, keyCh chan<- InboundKeyUpdate) {
	targets, inboundKeys, err := d.discover(ctx)
	if err != nil {
		d.log.Warn("Target discovery tick failed", "error", err)
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
}

// discover performs a single discovery cycle: fetch users, filter, extract targets/keys,
// merge with CLI values.
func (d *TargetDiscovery) discover(ctx context.Context) ([]ProbeAddress, [][32]byte, error) {
	d.tickCount++

	users, err := d.client.GetGeolocationUsers(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch GeolocationUser accounts: %w", err)
	}

	var probePKBytes [32]byte
	copy(probePKBytes[:], d.geoProbePubkey[:])

	var onchainTargets []ProbeAddress
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
			}
		}
	}

	mergedTargets := mergeProbes(d.cliTargets, onchainTargets)
	mergedKeys := mergeKeys(d.cliAllowedKeys, onchainKeys)

	d.log.Debug("Target discovery tick",
		"users", len(users),
		"onchainOutbound", len(onchainTargets),
		"onchainInbound", len(onchainKeys),
		"cliTargets", len(d.cliTargets),
		"cliKeys", len(d.cliAllowedKeys),
		"mergedTargets", len(mergedTargets),
		"mergedKeys", len(mergedKeys),
	)

	return mergedTargets, mergedKeys, nil
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
