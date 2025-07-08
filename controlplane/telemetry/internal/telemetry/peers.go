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
	"github.com/malbeclabs/doublezero/controlplane/agent/pkg/arista"
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
	Logger           *slog.Logger
	LocalDevicePK    solana.PublicKey
	ProgramClient    ServiceabilityProgramClient
	AristaEAPIClient *arista.EAPIClient
	TWAMPPort        uint16
	RefreshInterval  time.Duration
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
	if cfg.AristaEAPIClient == nil {
		return nil, errors.New("AristaEAPIClient is required")
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
			err := p.refresh(ctx)
			if err != nil {
				p.log.Error("failed to refresh peers", "error", err)
				continue
			}
		}
	}
}

func (p *ledgerPeerDiscovery) GetPeers() []*Peer {
	p.peersMu.RLock()
	defer p.peersMu.RUnlock()
	return slices.Clone(p.peers)
}

func (p *ledgerPeerDiscovery) refresh(ctx context.Context) error {
	if err := p.config.ProgramClient.Load(ctx); err != nil {
		return fmt.Errorf("failed to load program from ledger: %w", err)
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

	// Get the local tunnel target IPs from the Arista EAPI client.
	localTunnelTargetIP4s, err := p.config.AristaEAPIClient.GetLocalTunnelTargetIPs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get local tunnel ips: %w", err)
	}

	peers := make([]*Peer, 0)
	for _, link := range links {
		// Ignore links that are not yet activated.
		if link.Status != serviceability.LinkStatusActivated {
			continue
		}
		// Ignore links that don't have a valid tunnel net.
		if link.TunnelNet == [5]byte{0, 0, 0, 0, 0} {
			continue
		}

		linkPubkey := solana.PublicKeyFromBytes(link.PubKey[:])
		sideA := solana.PublicKeyFromBytes(link.SideAPubKey[:])
		sideZ := solana.PublicKeyFromBytes(link.SideZPubKey[:])

		var remote string
		if sideA.Equals(p.config.LocalDevicePK) {
			remote = sideZ.String()
		} else if sideZ.Equals(p.config.LocalDevicePK) {
			remote = sideA.String()
		} else {
			continue
		}

		device, ok := devices[remote]
		if !ok {
			p.log.Debug("Device not found", "targetDevicePubKey", remote)
			continue
		}

		tunnelNet := bytesToIP4Net(link.TunnelNet)

		// Find a local tunnel target IP that is within the link's tunnel net, and use it as the
		// target IP for the peer.
		// NOTE: This is a workaround to get the target IP for the peer until the specific tunnel
		// IP for each side is saved onchain with the link.
		var targetIP net.IP
		for _, localTunnelIP := range localTunnelTargetIP4s {
			if tunnelNet.Contains(localTunnelIP) {
				targetIP = localTunnelIP
				break
			}
		}
		if targetIP == nil {
			p.log.Debug("Target ip not found for link", "linkPubkey", linkPubkey, "targetDevicePubKey", remote, "tunnelNet", tunnelNet, "localTunnelTargetIP4s", localTunnelTargetIP4s)
			continue
		}

		peers = append(peers, &Peer{
			LinkPK:   linkPubkey,
			DevicePK: solana.PublicKeyFromBytes(device.PubKey[:]),
			DeviceAddr: &net.UDPAddr{
				IP:   targetIP,
				Port: int(p.config.TWAMPPort),
			},
		})
	}

	p.peers = peers
	p.log.Debug("Refreshed peers", "devices", len(devices), "links", len(links), "peers", len(peers), "localTunnelTargetIP4s", len(localTunnelTargetIP4s))

	return nil
}

func bytesToIP4Net(b [5]byte) *net.IPNet {
	ip := net.IPv4(b[0], b[1], b[2], b[3])
	mask := net.CIDRMask(int(b[4]), 32)
	return &net.IPNet{IP: ip.To4(), Mask: mask}
}
