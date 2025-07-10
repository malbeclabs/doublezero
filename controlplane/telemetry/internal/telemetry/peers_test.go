package telemetry_test

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netutil"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentTelemetry_PeerDiscovery_Ledger(t *testing.T) {
	t.Parallel()

	t.Run("successful peer discovery", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		localDevicePK := stringToPubkey("device1")

		serviceabilityProgram := &mockServiceabilityProgramClient{
			LoadFunc: func(ctx context.Context) error {
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{
					{PubKey: localDevicePK, PublicIp: [4]uint8{192, 168, 1, 1}},
					{PubKey: stringToPubkey("device2"), PublicIp: [4]uint8{192, 168, 1, 2}},
					{PubKey: stringToPubkey("device3"), PublicIp: [4]uint8{192, 168, 1, 3}},
					{PubKey: stringToPubkey("device4"), PublicIp: [4]uint8{192, 168, 1, 4}},
				}
			},
			GetLinksFunc: func() []serviceability.Link {
				return []serviceability.Link{
					{PubKey: stringToPubkey("link_1-2"), Status: serviceability.LinkStatusActivated, SideAPubKey: localDevicePK, SideZPubKey: stringToPubkey("device2"), TunnelNet: [5]uint8{10, 1, 1, 0, 31}},
					{PubKey: stringToPubkey("link_1-3"), Status: serviceability.LinkStatusActivated, SideAPubKey: localDevicePK, SideZPubKey: stringToPubkey("device3"), TunnelNet: [5]uint8{10, 1, 1, 2, 31}},
					{PubKey: stringToPubkey("link_2-1"), Status: serviceability.LinkStatusActivated, SideAPubKey: stringToPubkey("device2"), SideZPubKey: localDevicePK, TunnelNet: [5]uint8{10, 1, 1, 5, 31}},
					{PubKey: stringToPubkey("link_2-3"), Status: serviceability.LinkStatusActivated, SideAPubKey: stringToPubkey("device2"), SideZPubKey: stringToPubkey("device3"), TunnelNet: [5]uint8{10, 1, 1, 6, 31}},
				}
			},
		}

		localInterfacesByIP := map[string]netutil.Interface{
			"10.1.1.0": {
				Name: "tun1-2",
				Addrs: []net.Addr{
					&net.IPNet{IP: ipv4([4]uint8{10, 1, 1, 0}), Mask: net.CIDRMask(31, 32)},
				},
			},
			"10.1.1.2": {
				Name: "tun1-3",
				Addrs: []net.Addr{
					&net.IPNet{IP: ipv4([4]uint8{10, 1, 1, 2}), Mask: net.CIDRMask(31, 32)},
				},
			},
			"10.1.1.5": {
				Name: "tun2-1",
				Addrs: []net.Addr{
					&net.IPNet{IP: ipv4([4]uint8{10, 1, 1, 5}), Mask: net.CIDRMask(31, 32)},
				},
			},
			"10.1.1.100": {
				Name: "tunOther",
				Addrs: []net.Addr{
					&net.IPNet{IP: ipv4([4]uint8{10, 1, 1, 100}), Mask: net.CIDRMask(31, 32)},
				},
			},
		}

		config := &telemetry.LedgerPeerDiscoveryConfig{
			Logger:          log,
			LocalDevicePK:   localDevicePK,
			TWAMPPort:       12345,
			RefreshInterval: 100 * time.Millisecond,
			ProgramClient:   serviceabilityProgram,
			LocalNet: &netutil.MockLocalNet{
				InterfacesFunc: func() ([]netutil.Interface, error) {
					interfaces := make([]netutil.Interface, 0, len(localInterfacesByIP))
					for _, iface := range localInterfacesByIP {
						interfaces = append(interfaces, iface)
					}
					return interfaces, nil
				},
			},
		}

		peers, err := telemetry.NewLedgerPeerDiscovery(config)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		errCh := make(chan error, 1)
		go func() {
			errCh <- peers.Run(ctx)
		}()

		require.Eventually(t, func() bool {
			return len(peers.GetPeers()) == 3
		}, 2*time.Second, 100*time.Millisecond)

		cancel()
		assert.NoError(t, <-errCh)

		expected := []*telemetry.Peer{
			{
				LinkPK:   stringToPubkey("link_1-2"),
				DevicePK: stringToPubkey("device2"),
				Tunnel: &netutil.LocalTunnel{
					Interface: "tun1-2",
					SourceIP:  ipv4([4]uint8{10, 1, 1, 0}),
					TargetIP:  ipv4([4]uint8{10, 1, 1, 1}),
				},
				TWAMPPort: 12345,
			},
			{
				LinkPK:   stringToPubkey("link_1-3"),
				DevicePK: stringToPubkey("device3"),
				Tunnel: &netutil.LocalTunnel{
					Interface: "tun1-3",
					SourceIP:  ipv4([4]uint8{10, 1, 1, 2}),
					TargetIP:  ipv4([4]uint8{10, 1, 1, 3}),
				},
				TWAMPPort: 12345,
			},
			{
				LinkPK:   stringToPubkey("link_2-1"),
				DevicePK: stringToPubkey("device2"),
				Tunnel: &netutil.LocalTunnel{
					Interface: "tun2-1",
					SourceIP:  ipv4([4]uint8{10, 1, 1, 5}),
					TargetIP:  ipv4([4]uint8{10, 1, 1, 4}),
				},
				TWAMPPort: 12345,
			},
		}

		requireUnorderedEqual(t, expected, peers.GetPeers())
	})

	t.Run("includes not found tunnel as nil tunnel", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		localDevicePK := stringToPubkey("device1")

		serviceabilityProgram := &mockServiceabilityProgramClient{
			LoadFunc: func(ctx context.Context) error {
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{
					{PubKey: localDevicePK, PublicIp: [4]uint8{192, 168, 1, 1}},
					{PubKey: stringToPubkey("device2"), PublicIp: [4]uint8{192, 168, 1, 2}},
					{PubKey: stringToPubkey("device3"), PublicIp: [4]uint8{192, 168, 1, 3}},
					{PubKey: stringToPubkey("device4"), PublicIp: [4]uint8{192, 168, 1, 4}},
				}
			},
			GetLinksFunc: func() []serviceability.Link {
				return []serviceability.Link{
					{PubKey: stringToPubkey("link_1-2"), Status: serviceability.LinkStatusActivated, SideAPubKey: localDevicePK, SideZPubKey: stringToPubkey("device2"), TunnelNet: [5]uint8{10, 1, 1, 0, 31}},
					{PubKey: stringToPubkey("link_1-3"), Status: serviceability.LinkStatusActivated, SideAPubKey: localDevicePK, SideZPubKey: stringToPubkey("device3"), TunnelNet: [5]uint8{10, 1, 1, 2, 31}},
					{PubKey: stringToPubkey("link_2-1"), Status: serviceability.LinkStatusActivated, SideAPubKey: stringToPubkey("device2"), SideZPubKey: localDevicePK, TunnelNet: [5]uint8{10, 1, 1, 5, 31}},
					{PubKey: stringToPubkey("link_2-3"), Status: serviceability.LinkStatusActivated, SideAPubKey: stringToPubkey("device2"), SideZPubKey: stringToPubkey("device3"), TunnelNet: [5]uint8{10, 1, 1, 6, 31}},
				}
			},
		}

		localInterfaces := []netutil.Interface{
			{
				Name: "tun1-2",
				Addrs: []net.Addr{
					&net.IPNet{IP: ipv4([4]uint8{10, 1, 1, 0}), Mask: net.CIDRMask(31, 32)},
				},
			},
			{
				Name:  "tun1-3",
				Addrs: []net.Addr{},
			},
			{
				Name: "tun2-1",
				Addrs: []net.Addr{
					&net.IPNet{IP: ipv4([4]uint8{10, 1, 1, 5}), Mask: net.CIDRMask(31, 32)},
				},
			},
			{
				Name: "tunOther",
				Addrs: []net.Addr{
					&net.IPNet{IP: ipv4([4]uint8{10, 1, 1, 100}), Mask: net.CIDRMask(31, 32)},
				},
			},
		}

		config := &telemetry.LedgerPeerDiscoveryConfig{
			Logger:          log,
			LocalDevicePK:   localDevicePK,
			TWAMPPort:       12345,
			RefreshInterval: 100 * time.Millisecond,
			ProgramClient:   serviceabilityProgram,
			LocalNet: &netutil.MockLocalNet{
				InterfacesFunc: func() ([]netutil.Interface, error) {
					return localInterfaces, nil
				},
			},
		}

		peers, err := telemetry.NewLedgerPeerDiscovery(config)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		errCh := make(chan error, 1)
		go func() {
			errCh <- peers.Run(ctx)
		}()

		require.Eventually(t, func() bool {
			return len(peers.GetPeers()) == 3
		}, 2*time.Second, 100*time.Millisecond)

		cancel()
		assert.NoError(t, <-errCh)

		expected := []*telemetry.Peer{
			{
				LinkPK:   stringToPubkey("link_1-2"),
				DevicePK: stringToPubkey("device2"),
				Tunnel: &netutil.LocalTunnel{
					Interface: "tun1-2",
					SourceIP:  ipv4([4]uint8{10, 1, 1, 0}),
					TargetIP:  ipv4([4]uint8{10, 1, 1, 1}),
				},
				TWAMPPort: 12345,
			},
			{
				LinkPK:    stringToPubkey("link_1-3"),
				DevicePK:  stringToPubkey("device3"),
				Tunnel:    nil,
				TWAMPPort: 12345,
			},
			{
				LinkPK:   stringToPubkey("link_2-1"),
				DevicePK: stringToPubkey("device2"),
				Tunnel: &netutil.LocalTunnel{
					Interface: "tun2-1",
					SourceIP:  ipv4([4]uint8{10, 1, 1, 5}),
					TargetIP:  ipv4([4]uint8{10, 1, 1, 4}),
				},
				TWAMPPort: 12345,
			},
		}

		requireUnorderedEqual(t, expected, peers.GetPeers())
	})

	t.Run("skips pending links", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		localDevicePK := stringToPubkey("device1")

		serviceabilityProgram := &mockServiceabilityProgramClient{
			LoadFunc: func(ctx context.Context) error {
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{
					{PubKey: localDevicePK},
					{PubKey: stringToPubkey("device2")},
				}
			},
			GetLinksFunc: func() []serviceability.Link {
				return []serviceability.Link{
					{
						PubKey:      stringToPubkey("inactive_link"),
						Status:      serviceability.LinkStatusPending,
						SideAPubKey: localDevicePK,
						SideZPubKey: stringToPubkey("device2"),
						TunnelNet:   [5]uint8{10, 1, 2, 0, 31},
					},
				}
			},
		}

		cfg := &telemetry.LedgerPeerDiscoveryConfig{
			Logger:        log,
			LocalDevicePK: localDevicePK,
			ProgramClient: serviceabilityProgram,
			LocalNet: &netutil.MockLocalNet{
				InterfacesFunc: func() ([]netutil.Interface, error) {
					return []netutil.Interface{
						{Name: "tunX", Addrs: []net.Addr{&net.IPNet{IP: ipv4([4]uint8{10, 1, 2, 0}), Mask: net.CIDRMask(31, 32)}}},
					}, nil
				},
			},
			TWAMPPort:       1234,
			RefreshInterval: 50 * time.Millisecond,
		}

		peerDiscovery, err := telemetry.NewLedgerPeerDiscovery(cfg)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		errCh := make(chan error, 1)
		go func() {
			errCh <- peerDiscovery.Run(ctx)
		}()

		require.Never(t, func() bool {
			return len(peerDiscovery.GetPeers()) > 0
		}, 500*time.Millisecond, 50*time.Millisecond)

		cancel()
		assert.NoError(t, <-errCh)
	})

	t.Run("skips links with invalid tunnel_net", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		localDevicePK := stringToPubkey("device1")

		serviceabilityProgram := &mockServiceabilityProgramClient{
			LoadFunc: func(ctx context.Context) error {
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{
					{PubKey: localDevicePK},
					{PubKey: stringToPubkey("device2")},
				}
			},
			GetLinksFunc: func() []serviceability.Link {
				return []serviceability.Link{
					{
						PubKey:      stringToPubkey("bad_tunnel_net"),
						Status:      serviceability.LinkStatusActivated,
						SideAPubKey: localDevicePK,
						SideZPubKey: stringToPubkey("device2"),
						TunnelNet:   [5]uint8{0, 0, 0, 0, 0}, // invalid
					},
				}
			},
		}

		cfg := &telemetry.LedgerPeerDiscoveryConfig{
			Logger:        log,
			LocalDevicePK: localDevicePK,
			ProgramClient: serviceabilityProgram,
			LocalNet: &netutil.MockLocalNet{
				InterfacesFunc: func() ([]netutil.Interface, error) {
					return []netutil.Interface{
						{Name: "tunX", Addrs: []net.Addr{&net.IPNet{IP: ipv4([4]uint8{10, 2, 2, 0}), Mask: net.CIDRMask(31, 32)}}},
					}, nil
				},
			},
			TWAMPPort:       1234,
			RefreshInterval: 50 * time.Millisecond,
		}

		peerDiscovery, err := telemetry.NewLedgerPeerDiscovery(cfg)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		errCh := make(chan error, 1)
		go func() {
			errCh <- peerDiscovery.Run(ctx)
		}()

		require.Never(t, func() bool {
			return len(peerDiscovery.GetPeers()) > 0
		}, 500*time.Millisecond, 50*time.Millisecond)

		cancel()
		assert.NoError(t, <-errCh)
	})

	t.Run("removes_peer_when_link_removed_from_ledger", func(t *testing.T) {
		t.Parallel()

		log := slog.With("test", t.Name())
		localDevicePK := stringToPubkey("device1")
		device2PK := stringToPubkey("device2")
		linkPK := stringToPubkey("link_1-2")

		var mu sync.RWMutex
		state := struct {
			links   []serviceability.Link
			devices []serviceability.Device
		}{
			devices: []serviceability.Device{
				{PubKey: localDevicePK, PublicIp: [4]uint8{192, 168, 1, 1}},
				{PubKey: device2PK, PublicIp: [4]uint8{192, 168, 1, 2}},
			},
			links: []serviceability.Link{
				{PubKey: linkPK, Status: serviceability.LinkStatusActivated, SideAPubKey: localDevicePK, SideZPubKey: device2PK, TunnelNet: [5]uint8{10, 1, 1, 0, 31}},
			},
		}

		serviceabilityProgram := &mockServiceabilityProgramClient{
			LoadFunc: func(ctx context.Context) error {
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				mu.RLock()
				defer mu.RUnlock()
				return state.devices
			},
			GetLinksFunc: func() []serviceability.Link {
				mu.RLock()
				defer mu.RUnlock()
				return state.links
			},
		}

		config := &telemetry.LedgerPeerDiscoveryConfig{
			Logger:          log,
			LocalDevicePK:   localDevicePK,
			TWAMPPort:       12345,
			RefreshInterval: 50 * time.Millisecond,
			ProgramClient:   serviceabilityProgram,
			LocalNet: &netutil.MockLocalNet{
				InterfacesFunc: func() ([]netutil.Interface, error) {
					return []netutil.Interface{
						{Name: "tunX", Addrs: []net.Addr{
							&net.IPNet{IP: ipv4([4]uint8{10, 1, 1, 0}), Mask: net.CIDRMask(31, 32)},
							&net.IPNet{IP: ipv4([4]uint8{10, 1, 1, 1}), Mask: net.CIDRMask(31, 32)},
						}},
					}, nil
				},
			},
		}

		peers, err := telemetry.NewLedgerPeerDiscovery(config)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			require.NoError(t, peers.Run(ctx))
		}()

		// Wait for peer to appear
		require.Eventually(t, func() bool {
			return len(peers.GetPeers()) == 1
		}, 2*time.Second, 50*time.Millisecond, "peer should be initially discovered")

		// Remove the link (simulate on-chain deletion)
		mu.Lock()
		state.links = []serviceability.Link{}
		mu.Unlock()

		// Wait for peer to disappear
		require.Eventually(t, func() bool {
			return len(peers.GetPeers()) == 0
		}, 2*time.Second, 50*time.Millisecond, "peer should be removed after link disappears")
	})

	t.Run("invalid config", func(t *testing.T) {
		t.Parallel()

		base := func(cfg telemetry.LedgerPeerDiscoveryConfig, msg string) {
			t.Helper()
			t.Run(msg, func(t *testing.T) {
				t.Parallel()
				_, err := telemetry.NewLedgerPeerDiscovery(&cfg)
				assert.Error(t, err)
			})
		}

		valid := telemetry.LedgerPeerDiscoveryConfig{
			Logger:          slog.Default(),
			LocalDevicePK:   stringToPubkey("device1"),
			TWAMPPort:       1234,
			RefreshInterval: 100 * time.Millisecond,
			ProgramClient: &mockServiceabilityProgramClient{
				LoadFunc: func(ctx context.Context) error {
					return nil
				},
				GetDevicesFunc: func() []serviceability.Device {
					return []serviceability.Device{}
				},
				GetLinksFunc: func() []serviceability.Link {
					return []serviceability.Link{}
				},
			},
			LocalNet: &netutil.MockLocalNet{
				InterfacesFunc: func() ([]netutil.Interface, error) {
					return []netutil.Interface{}, nil
				},
			},
		}

		cfg := valid
		cfg.Logger = nil
		base(cfg, "nil logger")

		cfg = valid
		cfg.LocalDevicePK = solana.PublicKey{}
		base(cfg, "empty local device pubkey")

		cfg = valid
		cfg.ProgramClient = nil
		base(cfg, "nil serviceability client")

		cfg = valid
		cfg.LocalNet = nil
		base(cfg, "nil local net")

		cfg = valid
		cfg.TWAMPPort = 0
		base(cfg, "zero TWAMP port")

		cfg = valid
		cfg.RefreshInterval = 0
		base(cfg, "zero refresh interval")
	})
}

func ipv4(bytes [4]uint8) net.IP {
	return net.IP{bytes[0], bytes[1], bytes[2], bytes[3]}
}
