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
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/metrics"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netutil"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

type Peer struct {
	LinkPK    solana.PublicKey
	DevicePK  solana.PublicKey
	Tunnel    *netutil.LocalTunnel
	TWAMPPort uint16
}

func (p *Peer) String() string {
	targetIP := ""
	if p.Tunnel != nil {
		targetIP = p.Tunnel.TargetIP.String()
	}
	return fmt.Sprintf("device=%s,addr=%s,link=%s", p.DevicePK.String(), targetIP, p.LinkPK.String())
}

type PeerDiscovery interface {
	Run(ctx context.Context) error
	GetPeers() []*Peer
}

type LedgerPeerDiscoveryConfig struct {
	Logger          *slog.Logger
	LocalDevicePK   solana.PublicKey
	ProgramClient   ServiceabilityProgramClient
	LocalNet        netutil.LocalNet
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
	if cfg.LocalNet == nil {
		return nil, errors.New("LocalNet is required")
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
	p.log.Info("Starting peer discovery", "refreshInterval", p.config.RefreshInterval)
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
	data, err := p.config.ProgramClient.GetProgramData(ctx)
	if err != nil {
		metrics.Errors.WithLabelValues(metrics.ErrorTypePeerDiscoveryProgramLoad).Inc()
		return fmt.Errorf("failed to load program from ledger: %w", err)
	}

	p.peersMu.Lock()
	defer p.peersMu.Unlock()

	p.peers = make([]*Peer, 0, len(p.peers))

	devices := make(map[string]serviceability.Device)
	for _, device := range data.Devices {
		pubkey := solana.PublicKeyFromBytes(device.PubKey[:])
		devices[pubkey.String()] = device
	}

	links := make(map[string]serviceability.Link)
	for _, link := range data.Links {
		pubkey := solana.PublicKeyFromBytes(link.PubKey[:])
		links[pubkey.String()] = link
	}

	// Get all local interfaces.
	interfaces, err := p.config.LocalNet.Interfaces()
	if err != nil {
		metrics.Errors.WithLabelValues(metrics.ErrorTypePeerDiscoveryGettingLocalInterfaces).Inc()
		return fmt.Errorf("failed to get local interfaces: %w", err)
	}

	var tunnelsNotFound int

	peers := make([]*Peer, 0)
	for _, link := range links {
		// Ignore links that are not yet activated.
		if link.Status != serviceability.LinkStatusActivated &&
			link.Status != serviceability.LinkStatusSoftDrained &&
			link.Status != serviceability.LinkStatusHardDrained &&
			link.Status != serviceability.LinkStatusProvisioning {
			continue
		}
		// Ignore links that don't have a valid tunnel net.
		if link.TunnelNet == [5]byte{0, 0, 0, 0, 0} {
			metrics.Errors.WithLabelValues(metrics.ErrorTypePeerDiscoveryLinkTunnelNetInvalid).Inc()
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

		// Find a local tunnel target IP that is within the link's tunnel net, and use it as the
		// target IP for the peer.
		// NOTE: This is a workaround to get the target IP for the peer until the specific tunnel
		// IP for each side is saved onchain with the link.
		tunnelNet := bytesToIP4Net(link.TunnelNet)
		tunnel, err := netutil.FindLocalTunnel(interfaces, tunnelNet)
		if err != nil && !errors.Is(err, netutil.ErrLocalTunnelNotFound) {
			p.log.Debug("Failed to find local tunnel interface", "error", err, "linkPubkey", linkPubkey, "targetDevicePubKey", remote, "tunnelNet", tunnelNet)
			metrics.Errors.WithLabelValues(metrics.ErrorTypePeerDiscoveryFindingLocalTunnel).Inc()
			continue
		}
		// NOTE: If the tunnel was not found, it will be nil here, so downstream usage should check
		// for that.
		if tunnel == nil {
			tunnelsNotFound++
		}

		peers = append(peers, &Peer{
			LinkPK:    linkPubkey,
			DevicePK:  solana.PublicKeyFromBytes(device.PubKey[:]),
			Tunnel:    tunnel,
			TWAMPPort: p.config.TWAMPPort,
		})
	}

	p.peers = peers
	p.log.Debug("Refreshed peers", "devices", len(devices), "links", len(links), "peers", len(peers), "tunnelsNotFound", tunnelsNotFound)

	// Record the number of tunnels not found.
	metrics.PeerDiscoveryLocalTunnelNotFound.WithLabelValues(p.config.LocalDevicePK.String()).Set(float64(tunnelsNotFound))

	return nil
}

func bytesToIP4Net(b [5]byte) *net.IPNet {
	ip := net.IPv4(b[0], b[1], b[2], b[3])
	mask := net.CIDRMask(int(b[4]), 32)
	return &net.IPNet{IP: ip.To4(), Mask: mask}
}
