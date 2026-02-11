package geoprobe

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netns"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

const (
	LatLngCacheTTL = 24 * time.Hour
	SlotCacheTTL   = 5 * time.Minute
)

type PublisherConfig struct {
	Logger               *slog.Logger
	Keypair              solana.PrivateKey
	LocalDevicePK        solana.PublicKey
	ServiceabilityClient ServiceabilityClientInterface
	RPCClient            RPCClientInterface
	ManagementNamespace  string
}

type Publisher struct {
	log *slog.Logger
	cfg *PublisherConfig

	signer *OffsetSigner

	latLngMu       sync.RWMutex
	cachedLat      float64
	cachedLng      float64
	latLngCachedAt time.Time

	slotMu       sync.RWMutex
	cachedSlot   uint64
	slotCachedAt time.Time

	connsMu sync.Mutex
	conns   map[string]*probeConn
}

type probeConn struct {
	conn         *net.UDPConn
	resolvedAddr *net.UDPAddr
}

func NewPublisher(cfg *PublisherConfig) (*Publisher, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if cfg.Keypair == nil {
		return nil, fmt.Errorf("keypair is required")
	}
	if cfg.LocalDevicePK.IsZero() {
		return nil, fmt.Errorf("local device pubkey is required")
	}
	if cfg.ServiceabilityClient == nil {
		return nil, fmt.Errorf("serviceability client is required")
	}
	if cfg.RPCClient == nil {
		return nil, fmt.Errorf("rpc client is required")
	}

	return &Publisher{
		log:    cfg.Logger,
		cfg:    cfg,
		signer: NewOffsetSigner(cfg.Keypair),
		conns:  make(map[string]*probeConn),
	}, nil
}

func (p *Publisher) AddProbe(ctx context.Context, addr ProbeAddress) error {
	p.connsMu.Lock()
	defer p.connsMu.Unlock()

	key := addr.String()
	if _, exists := p.conns[key]; exists {
		p.log.Debug("probe already exists, skipping", "address", key)
		return nil
	}

	if err := addr.Validate(); err != nil {
		return fmt.Errorf("invalid probe address %s: %w", key, err)
	}

	var conn *net.UDPConn
	var err error

	createConn := func() (*net.UDPConn, error) {
		return NewUDPConn()
	}

	if p.cfg.ManagementNamespace != "" {
		conn, err = netns.RunInNamespace(p.cfg.ManagementNamespace, createConn)
	} else {
		conn, err = createConn()
	}

	if err != nil {
		return fmt.Errorf("failed to create UDP connection for probe %s: %w", key, err)
	}

	resolvedAddr := &net.UDPAddr{IP: net.ParseIP(addr.Host), Port: int(addr.Port)}

	p.conns[key] = &probeConn{
		conn:         conn,
		resolvedAddr: resolvedAddr,
	}

	p.log.Info("added probe", "address", key)
	return nil
}

func (p *Publisher) RemoveProbe(addr ProbeAddress) error {
	p.connsMu.Lock()
	defer p.connsMu.Unlock()

	key := addr.String()
	pc, exists := p.conns[key]
	if !exists {
		p.log.Debug("probe not found, skipping removal", "address", key)
		return nil
	}

	if err := pc.conn.Close(); err != nil {
		p.log.Warn("error closing connection for probe", "address", key, "error", err)
	}

	delete(p.conns, key)
	p.log.Info("removed probe", "address", key)
	return nil
}

func (p *Publisher) Publish(ctx context.Context, rttData map[ProbeAddress]uint64) error {
	if len(rttData) == 0 {
		return nil
	}

	lat, lng, err := p.getLatLng(ctx)
	if err != nil {
		return fmt.Errorf("failed to get lat/lng: %w", err)
	}

	slot, err := p.getCurrentSlot(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current slot: %w", err)
	}

	p.connsMu.Lock()
	defer p.connsMu.Unlock()

	var publishErrors []error
	var errorsMu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, MaxConcurrentProbes)

	for addr, rttNs := range rttData {
		wg.Add(1)
		go func(addr ProbeAddress, rttNs uint64) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			key := addr.String()
			pc, exists := p.conns[key]
			if !exists {
				p.log.Warn("skipping probe not in connection pool", "address", key)
				return
			}

			offset := LocationOffset{
				MeasurementSlot: slot,
				Lat:             lat,
				Lng:             lng,
				MeasuredRttNs:   rttNs,
				RttNs:           rttNs,
				NumReferences:   0,
				References:      []LocationOffset{},
			}

			if err := p.signer.SignOffset(&offset); err != nil {
				p.log.Error("failed to sign offset", "probe", key, "error", err)
				errorsMu.Lock()
				publishErrors = append(publishErrors, fmt.Errorf("sign offset for %s: %w", key, err))
				errorsMu.Unlock()
				return
			}

			if err := SendOffset(pc.conn, pc.resolvedAddr, &offset); err != nil {
				p.log.Error("failed to send offset", "probe", key, "error", err)
				errorsMu.Lock()
				publishErrors = append(publishErrors, fmt.Errorf("send offset to %s: %w", key, err))
				errorsMu.Unlock()
				return
			}

			p.log.Debug("sent offset to probe",
				"probe", key,
				"slot", slot,
				"rtt_ns", rttNs,
				"lat", lat,
				"lng", lng)
		}(addr, rttNs)
	}

	wg.Wait()

	if len(publishErrors) > 0 {
		return fmt.Errorf("encountered %d errors while publishing offsets", len(publishErrors))
	}

	return nil
}

func (p *Publisher) Close() error {
	p.connsMu.Lock()
	defer p.connsMu.Unlock()

	var closeErrors []error
	for key, pc := range p.conns {
		if err := pc.conn.Close(); err != nil {
			p.log.Warn("error closing connection", "probe", key, "error", err)
			closeErrors = append(closeErrors, err)
		}
	}

	p.conns = make(map[string]*probeConn)

	if len(closeErrors) > 0 {
		return fmt.Errorf("encountered %d errors while closing connections", len(closeErrors))
	}

	return nil
}

func (p *Publisher) getLatLng(ctx context.Context) (lat, lng float64, err error) {
	p.latLngMu.RLock()
	if !p.latLngCachedAt.IsZero() && time.Since(p.latLngCachedAt) < LatLngCacheTTL {
		lat, lng := p.cachedLat, p.cachedLng
		p.latLngMu.RUnlock()
		return lat, lng, nil
	}
	p.latLngMu.RUnlock()

	programData, err := p.cfg.ServiceabilityClient.GetProgramData(ctx)
	if err != nil {
		p.latLngMu.RLock()
		hasStaleCache := !p.latLngCachedAt.IsZero()
		if hasStaleCache {
			lat, lng := p.cachedLat, p.cachedLng
			p.latLngMu.RUnlock()
			p.log.Warn("failed to fetch device/location, using stale cache",
				"error", err,
				"cache_age", time.Since(p.latLngCachedAt))
			return lat, lng, nil
		}
		p.latLngMu.RUnlock()
		return 0, 0, fmt.Errorf("failed to get program data: %w", err)
	}

	var device *serviceability.Device
	for i := range programData.Devices {
		devicePK := solana.PublicKeyFromBytes(programData.Devices[i].PubKey[:])
		if devicePK.Equals(p.cfg.LocalDevicePK) {
			device = &programData.Devices[i]
			break
		}
	}

	if device == nil {
		p.latLngMu.RLock()
		hasStaleCache := !p.latLngCachedAt.IsZero()
		if hasStaleCache {
			lat, lng := p.cachedLat, p.cachedLng
			p.latLngMu.RUnlock()
			p.log.Warn("device not found in program data, using stale cache",
				"device_pk", p.cfg.LocalDevicePK,
				"cache_age", time.Since(p.latLngCachedAt))
			return lat, lng, nil
		}
		p.latLngMu.RUnlock()
		return 0, 0, fmt.Errorf("device %s not found in program data", p.cfg.LocalDevicePK)
	}

	locationPK := solana.PublicKeyFromBytes(device.LocationPubKey[:])

	var location *serviceability.Location
	for i := range programData.Locations {
		locPK := solana.PublicKeyFromBytes(programData.Locations[i].PubKey[:])
		if locPK.Equals(locationPK) {
			location = &programData.Locations[i]
			break
		}
	}

	if location == nil {
		p.latLngMu.RLock()
		hasStaleCache := !p.latLngCachedAt.IsZero()
		if hasStaleCache {
			lat, lng := p.cachedLat, p.cachedLng
			p.latLngMu.RUnlock()
			p.log.Warn("location not found in program data, using stale cache",
				"location_pk", locationPK,
				"cache_age", time.Since(p.latLngCachedAt))
			return lat, lng, nil
		}
		p.latLngMu.RUnlock()
		return 0, 0, fmt.Errorf("location %s not found in program data", locationPK)
	}

	p.latLngMu.Lock()
	p.cachedLat = location.Lat
	p.cachedLng = location.Lng
	p.latLngCachedAt = time.Now()
	lat, lng = p.cachedLat, p.cachedLng
	p.latLngMu.Unlock()

	p.log.Debug("refreshed lat/lng cache",
		"lat", lat,
		"lng", lng,
		"location_code", location.Code)

	return lat, lng, nil
}

func (p *Publisher) getCurrentSlot(ctx context.Context) (uint64, error) {
	slot, err := p.cfg.RPCClient.GetSlot(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		p.slotMu.RLock()
		hasValidCache := !p.slotCachedAt.IsZero() && time.Since(p.slotCachedAt) < SlotCacheTTL
		if hasValidCache {
			cachedSlot := p.cachedSlot
			p.slotMu.RUnlock()
			p.log.Warn("failed to fetch current slot, using cached slot",
				"error", err,
				"cached_slot", cachedSlot,
				"cache_age", time.Since(p.slotCachedAt))
			return cachedSlot, nil
		}
		p.slotMu.RUnlock()
		return 0, fmt.Errorf("failed to get slot from RPC: %w", err)
	}

	p.slotMu.Lock()
	p.cachedSlot = slot
	p.slotCachedAt = time.Now()
	p.slotMu.Unlock()

	return slot, nil
}
