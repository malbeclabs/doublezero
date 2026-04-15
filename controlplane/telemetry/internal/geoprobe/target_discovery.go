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
	Targets       []ProbeAddress
	DeliveryAddrs map[ProbeAddress]string // measurement target → "host:port" override (empty map = all send to target)
}

// InboundKeyUpdate contains inbound allowed pubkeys discovered from onchain data.
type InboundKeyUpdate struct {
	Keys [][32]byte
}

// ICMPTargetUpdate contains outbound ICMP probe targets discovered from onchain data.
type ICMPTargetUpdate struct {
	Targets       []ProbeAddress
	DeliveryAddrs map[ProbeAddress]string // measurement target → "host:port" override (empty map = no listener)
}

// targetDiscoveryFullRefreshEvery controls how often a full GeolocationUser scan
// is forced regardless of whether the GeoProbe target_update_count has changed.
// At the default 60s interval, 5 means a full refresh every ~5 minutes.
const targetDiscoveryFullRefreshEvery = 5

// TargetDiscoveryConfig holds configuration for target discovery.
type TargetDiscoveryConfig struct {
	GeoProbePubkey         solana.PublicKey
	Client                 GeolocationUserClient
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
	probeTargetUpdateCount *atomic.Uint32

	cachedTargets             []ProbeAddress
	cachedIcmpTargets         []ProbeAddress
	cachedInboundKeys         [][32]byte
	cachedOutboundDelivery    map[ProbeAddress]string
	cachedIcmpDelivery        map[ProbeAddress]string
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
		probeTargetUpdateCount: cfg.ProbeTargetUpdateCount,
	}, nil
}

// Tick performs a single target discovery cycle and sends updates to the channels.
func (d *TargetDiscovery) Tick(ctx context.Context, targetCh chan<- TargetUpdate, keyCh chan<- InboundKeyUpdate, icmpTargetCh chan<- ICMPTargetUpdate) {
	d.discoverAndSend(ctx, targetCh, keyCh, icmpTargetCh)
}

func (d *TargetDiscovery) discoverAndSend(ctx context.Context, targetCh chan<- TargetUpdate, keyCh chan<- InboundKeyUpdate, icmpTargetCh chan<- ICMPTargetUpdate) {
	targets, icmpTargets, inboundKeys, deliveryAddrs, err := d.discover(ctx)
	if err != nil {
		d.log.Warn("Target discovery tick failed", "error", err)
		return
	}

	// nil targets means the scan was skipped (target_update_count unchanged).
	if targets == nil && inboundKeys == nil && icmpTargets == nil {
		return
	}

	// Split delivery addrs by target type.
	outboundDelivery := make(map[ProbeAddress]string)
	icmpDelivery := make(map[ProbeAddress]string)
	outboundSet := make(map[ProbeAddress]struct{}, len(targets))
	for _, t := range targets {
		outboundSet[t] = struct{}{}
	}
	for addr, dest := range deliveryAddrs {
		if _, ok := outboundSet[addr]; ok {
			outboundDelivery[addr] = dest
		} else {
			icmpDelivery[addr] = dest
		}
	}

	if !probeAddressSlicesEqual(targets, d.cachedTargets) || !deliveryAddrsEqual(outboundDelivery, d.cachedOutboundDelivery) {
		d.cachedTargets = targets
		d.cachedOutboundDelivery = outboundDelivery
		select {
		case targetCh <- TargetUpdate{Targets: targets, DeliveryAddrs: outboundDelivery}:
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

	if !probeAddressSlicesEqual(icmpTargets, d.cachedIcmpTargets) || !deliveryAddrsEqual(icmpDelivery, d.cachedIcmpDelivery) {
		d.cachedIcmpTargets = icmpTargets
		d.cachedIcmpDelivery = icmpDelivery
		select {
		case icmpTargetCh <- ICMPTargetUpdate{Targets: icmpTargets, DeliveryAddrs: icmpDelivery}:
		default:
			d.log.Warn("ICMP target update channel full, skipping update")
		}
	}
}

// discover performs a single discovery cycle: fetch users, filter, extract targets/keys,
// merge with CLI values. Returns nil, nil, nil, nil, nil when the scan is skipped.
// The returned deliveryAddrs maps measurement target → result destination for all targets
// whose user has a non-empty ResultDestination.
func (d *TargetDiscovery) discover(ctx context.Context) ([]ProbeAddress, []ProbeAddress, [][32]byte, map[ProbeAddress]string, error) {
	forceFullRefresh := d.tickCount%targetDiscoveryFullRefreshEvery == 0
	d.tickCount++

	if d.probeTargetUpdateCount != nil && !forceFullRefresh {
		current := d.probeTargetUpdateCount.Load()
		if current == d.lastSeenTargetUpdateCount && d.tickCount > 1 {
			d.log.Debug("GeoProbe target_update_count unchanged, skipping target scan",
				"targetUpdateCount", current)
			return nil, nil, nil, nil, nil
		}
		d.lastSeenTargetUpdateCount = current
	}

	users, err := d.client.GetGeolocationUsers(ctx)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to fetch GeolocationUser accounts: %w", err)
	}

	var probePKBytes [32]byte
	copy(probePKBytes[:], d.geoProbePubkey[:])

	var onchainTargets []ProbeAddress
	var onchainIcmpTargets []ProbeAddress
	var onchainKeys [][32]byte
	deliveryAddrs := make(map[ProbeAddress]string)
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

		resultDest := user.ResultDestination

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
				if resultDest != "" {
					deliveryAddrs[addr] = resultDest
				}

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
				if resultDest != "" {
					deliveryAddrs[addr] = resultDest
				}
			}
		}
	}

	// Sync lastSeenTargetUpdateCount after a full scan (covers forced refresh path).
	if d.probeTargetUpdateCount != nil {
		d.lastSeenTargetUpdateCount = d.probeTargetUpdateCount.Load()
	}

	d.log.Debug("Target discovery tick",
		"users", len(users),
		"onchainOutbound", len(onchainTargets),
		"onchainOutboundIcmp", len(onchainIcmpTargets),
		"onchainInbound", len(onchainKeys),
		"deliveryOverrides", len(deliveryAddrs),
	)

	return onchainTargets, onchainIcmpTargets, onchainKeys, deliveryAddrs, nil
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

// deliveryAddrsEqual checks if two delivery address maps are equal.
func deliveryAddrsEqual(a, b map[ProbeAddress]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
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
