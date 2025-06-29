package telemetry_test

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentTelemetry_PeerDiscovery_Ledger(t *testing.T) {
	t.Parallel()

	t.Run("successful peer discovery", func(t *testing.T) {
		t.Parallel()

		log := slog.With("test", t.Name())
		localDevicePK := stringToPubkey("device1")

		serviceabilityProgram := newMockServiceabilityProgramClient(func(c *mockServiceabilityProgramClient) error {
			c.devices = []serviceability.Device{
				{PubKey: localDevicePK, PublicIp: [4]uint8{192, 168, 1, 1}},
				{PubKey: stringToPubkey("device2"), PublicIp: [4]uint8{192, 168, 1, 2}},
				{PubKey: stringToPubkey("device3"), PublicIp: [4]uint8{192, 168, 1, 3}},
				{PubKey: stringToPubkey("device4"), PublicIp: [4]uint8{192, 168, 1, 4}},
			}
			c.links = []serviceability.Link{
				{PubKey: stringToPubkey("link_1-2"), SideAPubKey: localDevicePK, SideZPubKey: stringToPubkey("device2")},
				{PubKey: stringToPubkey("link_1-3"), SideAPubKey: localDevicePK, SideZPubKey: stringToPubkey("device3")},
				{PubKey: stringToPubkey("link_2-1"), SideAPubKey: stringToPubkey("device2"), SideZPubKey: localDevicePK},
				{PubKey: stringToPubkey("link_2-3"), SideAPubKey: stringToPubkey("device2"), SideZPubKey: stringToPubkey("device3")},
			}
			return nil
		})

		config := &telemetry.LedgerPeerDiscoveryConfig{
			Logger:          log,
			LocalDevicePK:   localDevicePK,
			TWAMPPort:       12345,
			RefreshInterval: 100 * time.Millisecond,
			ProgramClient:   serviceabilityProgram,
		}

		peers, err := telemetry.NewLedgerPeerDiscovery(config)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() {
			require.NoError(t, peers.Run(ctx))
		}()

		require.Eventually(t, func() bool {
			peers := peers.GetPeers()
			return len(peers) == 3
		}, 2*time.Second, 100*time.Millisecond)

		links := serviceabilityProgram.GetLinks()
		expected := map[string]*telemetry.Peer{
			solana.PublicKeyFromBytes(links[0].PubKey[:]).String(): {
				LinkPK:     stringToPubkey("link_1-2"),
				DevicePK:   stringToPubkey("device2"),
				DeviceAddr: &net.UDPAddr{IP: ipv4([4]uint8{192, 168, 1, 2}), Port: 12345},
			},
			solana.PublicKeyFromBytes(links[1].PubKey[:]).String(): {
				LinkPK:     stringToPubkey("link_1-3"),
				DevicePK:   stringToPubkey("device3"),
				DeviceAddr: &net.UDPAddr{IP: ipv4([4]uint8{192, 168, 1, 3}), Port: 12345},
			},
			solana.PublicKeyFromBytes(links[2].PubKey[:]).String(): {
				LinkPK:     stringToPubkey("link_2-1"),
				DevicePK:   stringToPubkey("device2"),
				DeviceAddr: &net.UDPAddr{IP: ipv4([4]uint8{192, 168, 1, 2}), Port: 12345},
			},
		}

		require.Len(t, peers.GetPeers(), len(expected))
		for _, peer := range peers.GetPeers() {
			assert.Equal(t, expected[peer.LinkPK.String()], peer)
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		t.Parallel()

		base := func(cfg telemetry.LedgerPeerDiscoveryConfig, msg string) {
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
			ProgramClient:   newMockServiceabilityProgramClient(func(c *mockServiceabilityProgramClient) error { return nil }),
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
