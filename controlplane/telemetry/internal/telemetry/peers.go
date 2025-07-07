package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type Peer struct {
	LinkPK     solana.PublicKey
	DevicePK   solana.PublicKey
	DeviceAddr *net.UDPAddr
}

func (p *Peer) String() string {
	return fmt.Sprintf("device=%s,addr=%s,link=%s", p.DevicePK.String(), p.DeviceAddr.String(), p.LinkPK.String())
}

type PeerDiscovery interface {
	Run(ctx context.Context) error
	GetPeers() []*Peer
}

type LedgerPeerDiscoveryConfig struct {
	Logger          *slog.Logger
	LocalDevicePK   solana.PublicKey
	ProgramClient   ServiceabilityProgramClient
	TWAMPPort       uint16
	RefreshInterval time.Duration
}

// ledgerPeerDiscovery implements the PeerDiscovery interface by periodically
// querying the on-chain serviceability program to discover peers.
//
// It maintains a cache of reachable peers (other devices linked to the local device)
// and updates this cache at a configurable interval. Each peer corresponds to a remote
// device that shares a link with the local device, and is identified by a public key
// and associated UDP address.
type ledgerPeerDiscovery struct {
	log     *slog.Logger
	config  *LedgerPeerDiscoveryConfig
	peers   []*Peer
	peersMu sync.RWMutex
}

func NewLedgerPeerDiscovery(cfg *LedgerPeerDiscoveryConfig) (*ledgerPeerDiscovery, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if cfg.Logger == nil {
		return nil, errors.New("logger is required")
	}
	if cfg.LocalDevicePK.IsZero() {
		return nil, errors.New("LocalDevicePK is required")
	}
	if cfg.ProgramClient == nil {
		return nil, errors.New("ProgramClient is required")
	}
	if cfg.TWAMPPort == 0 {
		return nil, errors.New("TWAMPPort is required")
	}
	if cfg.RefreshInterval == 0 {
		return nil, errors.New("RefreshInterval is required")
	}

	return &ledgerPeerDiscovery{
		log:    cfg.Logger,
		config: cfg,
		peers:  make([]*Peer, 0),
	}, nil
}

func (p *ledgerPeerDiscovery) Run(ctx context.Context) error {
	p.log.Info("Starting peer discovery")
	ticker := time.NewTicker(p.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.refresh(ctx)
		}
	}
}

func (p *ledgerPeerDiscovery) GetPeers() []*Peer {
	p.peersMu.RLock()
	defer p.peersMu.RUnlock()
	return slices.Clone(p.peers)
}

func (p *ledgerPeerDiscovery) refresh(ctx context.Context) {
	if err := p.config.ProgramClient.Load(ctx); err != nil {
		p.log.Error("Failed to load program from ledger", "error", err)
		return
	}

	p.peersMu.Lock()
	defer p.peersMu.Unlock()

	p.peers = make([]*Peer, 0, len(p.peers))

	devices := make(map[string]serviceability.Device)
	for _, device := range p.config.ProgramClient.GetDevices() {
		pubkey := solana.PublicKeyFromBytes(device.PubKey[:])
		devices[pubkey.String()] = device
	}

	links := make(map[string]serviceability.Link)
	for _, link := range p.config.ProgramClient.GetLinks() {
		pubkey := solana.PublicKeyFromBytes(link.PubKey[:])
		links[pubkey.String()] = link
	}

	peers := make([]*Peer, 0)
	for _, link := range links {
		linkPubkey := solana.PublicKeyFromBytes(link.PubKey[:])
		sideA := solana.PublicKeyFromBytes(link.SideAPubKey[:])
		sideB := solana.PublicKeyFromBytes(link.SideZPubKey[:])

		var remote string
		if sideA.Equals(p.config.LocalDevicePK) {
			remote = sideB.String()
		} else if sideB.Equals(p.config.LocalDevicePK) {
			remote = sideA.String()
		} else {
			continue
		}

		device, ok := devices[remote]
		if !ok {
			p.log.Debug("device not found", "targetDevicePubKey", remote)
			continue
		}

		peers = append(peers, &Peer{
			LinkPK:   linkPubkey,
			DevicePK: solana.PublicKeyFromBytes(device.PubKey[:]),
			DeviceAddr: &net.UDPAddr{
				IP:   net.IP(device.PublicIp[:]),
				Port: int(p.config.TWAMPPort),
			},
		})
	}

	p.log.Debug("Refreshed peers", "devices", len(devices), "links", len(links), "peers", len(peers))
	p.peers = peers
}
