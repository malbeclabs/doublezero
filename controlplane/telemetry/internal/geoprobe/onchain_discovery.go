package geoprobe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	serviceability "github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

// parentDiscoveryFullRefreshEvery controls how often a full device resolution
// is forced regardless of whether the parent device set has changed.
// At the default 60s interval, 5 means a full refresh every ~5 minutes.
const parentDiscoveryFullRefreshEvery = 5

// GeoProbeAccountClient fetches this probe's GeoProbe account by its onchain pubkey.
type GeoProbeAccountClient interface {
	GetGeoProbeByPubkey(ctx context.Context, pubkey solana.PublicKey) (*geolocation.GeoProbe, error)
}

// DeviceResolver resolves a Serviceability Device account by its onchain pubkey.
type DeviceResolver interface {
	GetDevice(ctx context.Context, pubkey solana.PublicKey) (*serviceability.Device, error)
}

// ParentUpdate is sent through the channel when parent DZDs change.
type ParentUpdate struct {
	// Authorities maps parent device pubkey → metrics publisher pubkey.
	Authorities map[[32]byte][32]byte
	// AllowedKeys holds metrics publisher pubkeys for signed TWAMP authorization.
	AllowedKeys [][32]byte
}

// ParentDiscoveryConfig holds configuration for parent discovery.
type ParentDiscoveryConfig struct {
	GeoProbePubkey         solana.PublicKey
	Client                 GeoProbeAccountClient
	Resolver               DeviceResolver
	CLIParents             map[[32]byte][32]byte // static parents from --additional-parent
	Logger                 *slog.Logger
	ProbeTargetUpdateCount *atomic.Uint32 // shared counter for target discovery change detection
}

// ParentDiscovery polls the GeoProbe account and resolves parent devices.
type ParentDiscovery struct {
	log                    *slog.Logger
	geoProbePubkey         solana.PublicKey
	client                 GeoProbeAccountClient
	resolver               DeviceResolver
	cliParents             map[[32]byte][32]byte
	probeTargetUpdateCount *atomic.Uint32

	cachedParentDevices []solana.PublicKey
	tickCount           uint64
}

// NewParentDiscovery creates a new ParentDiscovery instance.
func NewParentDiscovery(cfg *ParentDiscoveryConfig) (*ParentDiscovery, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if cfg.Client == nil {
		return nil, fmt.Errorf("geoprobe account client is required")
	}
	if cfg.Resolver == nil {
		return nil, fmt.Errorf("device resolver is required")
	}
	if cfg.GeoProbePubkey.IsZero() {
		return nil, fmt.Errorf("geoprobe pubkey is required")
	}
	cliParents := cfg.CLIParents
	if cliParents == nil {
		cliParents = make(map[[32]byte][32]byte)
	}

	return &ParentDiscovery{
		log:                    cfg.Logger,
		geoProbePubkey:         cfg.GeoProbePubkey,
		client:                 cfg.Client,
		resolver:               cfg.Resolver,
		cliParents:             cliParents,
		probeTargetUpdateCount: cfg.ProbeTargetUpdateCount,
	}, nil
}

// Tick performs a single parent discovery cycle and sends updates to the channel.
func (d *ParentDiscovery) Tick(ctx context.Context, ch chan<- ParentUpdate) {
	d.discoverAndSend(ctx, ch)
}

func (d *ParentDiscovery) discoverAndSend(ctx context.Context, ch chan<- ParentUpdate) {
	update, err := d.discover(ctx)
	if err != nil {
		d.log.Warn("Parent discovery tick failed", "error", err)
		return
	}
	if update == nil {
		return
	}

	select {
	case ch <- *update:
	default:
		d.log.Warn("Parent update channel full, skipping update")
	}
}

// discover performs a single discovery cycle.
func (d *ParentDiscovery) discover(ctx context.Context) (*ParentUpdate, error) {
	forceFullRefresh := d.tickCount%parentDiscoveryFullRefreshEvery == 0
	d.tickCount++

	probe, err := d.client.GetGeoProbeByPubkey(ctx, d.geoProbePubkey)
	if err != nil {
		if errors.Is(err, geolocation.ErrAccountNotFound) {
			d.log.Warn("GeoProbe account not found onchain, using CLI parents only",
				"geoProbePubkey", d.geoProbePubkey)
			return d.cliOnlyUpdate(), nil
		}
		return nil, fmt.Errorf("failed to fetch GeoProbe account: %w", err)
	}
	if probe == nil {
		return d.cliOnlyUpdate(), nil
	}

	// Publish the probe's target_update_count for target discovery change detection.
	if d.probeTargetUpdateCount != nil {
		d.probeTargetUpdateCount.Store(probe.TargetUpdateCount)
	}

	// Check if parent device set changed since last poll.
	if !forceFullRefresh && pubkeySlicesEqual(d.cachedParentDevices, probe.ParentDevices) {
		d.log.Debug("Parent device set unchanged, skipping resolution",
			"parentCount", len(probe.ParentDevices))
		return nil, nil
	}

	// Resolve each parent device.
	authorities := make(map[[32]byte][32]byte, len(probe.ParentDevices)+len(d.cliParents))
	var allowedKeys [][32]byte
	var zeroKey [32]byte
	resolvedCount := 0

	for _, parentPK := range probe.ParentDevices {
		device, err := d.resolver.GetDevice(ctx, parentPK)
		if err != nil {
			d.log.Warn("Failed to resolve parent device, skipping",
				"parentPubkey", parentPK, "error", err)
			continue
		}

		if device.MetricsPublisherPubKey == zeroKey {
			d.log.Warn("Parent device has zero MetricsPublisherPubKey, skipping",
				"parentPubkey", parentPK)
			continue
		}

		authorities[parentPK] = device.MetricsPublisherPubKey
		allowedKeys = append(allowedKeys, device.MetricsPublisherPubKey)
		resolvedCount++
		d.log.Debug("Resolved parent DZD",
			"parentPubkey", parentPK,
			"metricsPublisherPK", solana.PublicKeyFromBytes(device.MetricsPublisherPubKey[:]),
			"publicIP", fmt.Sprintf("%d.%d.%d.%d",
				device.PublicIp[0], device.PublicIp[1],
				device.PublicIp[2], device.PublicIp[3]))
	}

	// Merge CLI parents (always included, onchain takes precedence for same key).
	for pk, authPK := range d.cliParents {
		if _, exists := authorities[pk]; !exists {
			authorities[pk] = authPK
			allowedKeys = append(allowedKeys, authPK)
		}
	}

	d.cachedParentDevices = make([]solana.PublicKey, len(probe.ParentDevices))
	copy(d.cachedParentDevices, probe.ParentDevices)

	d.log.Debug("Parent discovery tick",
		"onchainParents", len(probe.ParentDevices),
		"resolvedOnchain", resolvedCount,
		"cliParents", len(d.cliParents),
		"totalAuthorities", len(authorities),
	)

	return &ParentUpdate{
		Authorities: authorities,
		AllowedKeys: allowedKeys,
	}, nil
}

// cliOnlyUpdate builds a ParentUpdate containing only CLI-configured parents.
func (d *ParentDiscovery) cliOnlyUpdate() *ParentUpdate {
	if len(d.cliParents) == 0 {
		return &ParentUpdate{
			Authorities: make(map[[32]byte][32]byte),
		}
	}
	authorities := make(map[[32]byte][32]byte, len(d.cliParents))
	var allowedKeys [][32]byte
	for pk, authPK := range d.cliParents {
		authorities[pk] = authPK
		allowedKeys = append(allowedKeys, authPK)
	}
	return &ParentUpdate{
		Authorities: authorities,
		AllowedKeys: allowedKeys,
	}
}

// pubkeySlicesEqual checks if two pubkey slices contain the same elements
// in the same order.
func pubkeySlicesEqual(a, b []solana.PublicKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rpcGeoProbeClient implements GeoProbeAccountClient using direct RPC calls.
type rpcGeoProbeClient struct {
	rpc       geolocation.RPCClient
	programID solana.PublicKey
}

// NewRPCGeoProbeClient creates a GeoProbeAccountClient backed by Solana RPC.
func NewRPCGeoProbeClient(rpc geolocation.RPCClient, programID solana.PublicKey) GeoProbeAccountClient {
	return &rpcGeoProbeClient{rpc: rpc, programID: programID}
}

func (c *rpcGeoProbeClient) GetGeoProbeByPubkey(ctx context.Context, pubkey solana.PublicKey) (*geolocation.GeoProbe, error) {
	result, err := c.rpc.GetAccountInfo(ctx, pubkey)
	if err != nil {
		if errors.Is(err, solanarpc.ErrNotFound) {
			return nil, geolocation.ErrAccountNotFound
		}
		return nil, fmt.Errorf("failed to get account info: %w", err)
	}
	if result.Value == nil {
		return nil, geolocation.ErrAccountNotFound
	}

	if result.Value.Owner != c.programID {
		return nil, fmt.Errorf("account %s is owned by %s, expected geolocation program %s",
			pubkey, result.Value.Owner, c.programID)
	}

	probe, err := geolocation.DeserializeGeoProbe(result.Value.Data.GetBinary())
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize GeoProbe: %w", err)
	}
	return probe, nil
}

// rpcDeviceResolver implements DeviceResolver using direct RPC calls.
type rpcDeviceResolver struct {
	rpc       geolocation.RPCClient
	programID solana.PublicKey
}

// NewRPCDeviceResolver creates a DeviceResolver backed by Solana RPC.
func NewRPCDeviceResolver(rpc geolocation.RPCClient, programID solana.PublicKey) DeviceResolver {
	return &rpcDeviceResolver{rpc: rpc, programID: programID}
}

func (r *rpcDeviceResolver) GetDevice(ctx context.Context, pubkey solana.PublicKey) (*serviceability.Device, error) {
	result, err := r.rpc.GetAccountInfo(ctx, pubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to get account info for device %s: %w", pubkey, err)
	}
	if result.Value == nil {
		return nil, fmt.Errorf("device account %s not found", pubkey)
	}

	if result.Value.Owner != r.programID {
		return nil, fmt.Errorf("device account %s is owned by %s, expected serviceability program %s",
			pubkey, result.Value.Owner, r.programID)
	}

	data := result.Value.Data.GetBinary()
	if len(data) == 0 {
		return nil, fmt.Errorf("device account %s has empty data", pubkey)
	}

	reader := serviceability.NewByteReader(data)
	var device serviceability.Device
	serviceability.DeserializeDevice(reader, &device)
	device.PubKey = pubkey

	return &device, nil
}
