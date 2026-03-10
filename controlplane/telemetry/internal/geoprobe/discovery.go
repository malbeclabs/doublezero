package geoprobe

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
)

// GeolocationClient is the interface for querying GeoProbe accounts from the
// onchain Geolocation program.
type GeolocationClient interface {
	GetGeoProbes(ctx context.Context) ([]geolocation.GeoProbe, error)
}

// DiscoveryConfig holds configuration for the probe discovery loop.
type DiscoveryConfig struct {
	Logger        *slog.Logger
	Client        GeolocationClient
	LocalDevicePK solana.PublicKey
	InitialProbes []ProbeAddress
	ProbeUpdateCh chan<- []ProbeAddress
	Interval      time.Duration
}

// Discovery periodically queries onchain GeoProbe accounts and sends updated
// probe lists to the Coordinator via ProbeUpdateCh.
type Discovery struct {
	log           *slog.Logger
	client        GeolocationClient
	localDevicePK solana.PublicKey
	initialProbes []ProbeAddress
	probeUpdateCh chan<- []ProbeAddress
	interval      time.Duration
}

// NewDiscovery creates a new Discovery instance.
func NewDiscovery(cfg *DiscoveryConfig) (*Discovery, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if cfg.Client == nil {
		return nil, fmt.Errorf("geolocation client is required")
	}
	if cfg.LocalDevicePK.IsZero() {
		return nil, fmt.Errorf("local device pubkey is required")
	}
	if cfg.ProbeUpdateCh == nil {
		return nil, fmt.Errorf("probe update channel is required")
	}
	if cfg.Interval <= 0 {
		return nil, fmt.Errorf("interval must be greater than 0")
	}

	return &Discovery{
		log:           cfg.Logger,
		client:        cfg.Client,
		localDevicePK: cfg.LocalDevicePK,
		initialProbes: cfg.InitialProbes,
		probeUpdateCh: cfg.ProbeUpdateCh,
		interval:      cfg.Interval,
	}, nil
}

// Run starts the discovery loop. It performs an immediate discovery tick, then
// repeats at the configured interval until the context is cancelled.
func (d *Discovery) Run(ctx context.Context) error {
	d.log.Info("Starting geoprobe discovery",
		"interval", d.interval,
		"localDevicePK", d.localDevicePK,
		"initialProbes", len(d.initialProbes),
	)

	d.discover(ctx)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.log.Info("Geoprobe discovery shutting down")
			return nil
		case <-ticker.C:
			d.discover(ctx)
		}
	}
}

func (d *Discovery) discover(ctx context.Context) {
	onchainProbes, err := d.client.GetGeoProbes(ctx)
	if err != nil {
		d.log.Warn("Failed to fetch onchain GeoProbe accounts", "error", err)
		return
	}

	var matched []ProbeAddress
	for i := range onchainProbes {
		if !hasParentDevice(&onchainProbes[i], d.localDevicePK) {
			continue
		}
		addr := GeoProbeToAddress(&onchainProbes[i])
		if err := addr.Validate(); err != nil {
			d.log.Warn("Skipping invalid onchain GeoProbe address",
				"code", onchainProbes[i].Code, "addr", addr, "error", err)
			continue
		}
		matched = append(matched, addr)
	}

	merged := mergeProbes(d.initialProbes, matched)

	d.log.Debug("Geoprobe discovery tick",
		"onchainTotal", len(onchainProbes),
		"onchainMatched", len(matched),
		"cliProbes", len(d.initialProbes),
		"merged", len(merged),
	)

	select {
	case d.probeUpdateCh <- merged:
	default:
		d.log.Debug("Probe update channel full, skipping update")
	}
}

// GeoProbeToAddress converts a GeoProbe account to a ProbeAddress.
func GeoProbeToAddress(probe *geolocation.GeoProbe) ProbeAddress {
	host := fmt.Sprintf("%d.%d.%d.%d",
		probe.PublicIP[0], probe.PublicIP[1],
		probe.PublicIP[2], probe.PublicIP[3])
	return ProbeAddress{
		Host:      host,
		Port:      probe.LocationOffsetPort,
		TWAMPPort: telemetryconfig.DefaultGeoprobeTWAMPPort,
	}
}

func hasParentDevice(probe *geolocation.GeoProbe, devicePK solana.PublicKey) bool {
	for _, parent := range probe.ParentDevices {
		if parent == devicePK {
			return true
		}
	}
	return false
}

// mergeProbes combines two sets of probes, deduplicating by ProbeAddress.String().
func mergeProbes(a, b []ProbeAddress) []ProbeAddress {
	seen := make(map[string]struct{}, len(a)+len(b))
	merged := make([]ProbeAddress, 0, len(a)+len(b))

	for _, addr := range a {
		key := addr.String()
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			merged = append(merged, addr)
		}
	}
	for _, addr := range b {
		key := addr.String()
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			merged = append(merged, addr)
		}
	}
	return merged
}
