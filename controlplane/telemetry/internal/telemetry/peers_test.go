package telemetry_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	aristapb "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/arista"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
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
					{PubKey: stringToPubkey("link_1-2"), Status: serviceability.LinkStatusActivated, SideAPubKey: localDevicePK, SideZPubKey: stringToPubkey("device2")},
					{PubKey: stringToPubkey("link_1-3"), Status: serviceability.LinkStatusActivated, SideAPubKey: localDevicePK, SideZPubKey: stringToPubkey("device3")},
					{PubKey: stringToPubkey("link_2-1"), Status: serviceability.LinkStatusActivated, SideAPubKey: stringToPubkey("device2"), SideZPubKey: localDevicePK},
					{PubKey: stringToPubkey("link_2-3"), Status: serviceability.LinkStatusActivated, SideAPubKey: stringToPubkey("device2"), SideZPubKey: stringToPubkey("device3")},
				}
			},
		}

		aristaEAPIClient := &arista.MockEAPIClient{
			RunShowCmdFunc: func(ctx context.Context, req *aristapb.RunShowCmdRequest, opts ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
				resp := arista.IPInterfacesBriefResponse{
					Interfaces: map[string]arista.IPInterfaceBrief{
						"Tunnel1-2": {
							InterfaceStatus:    arista.IPInterfaceInterfaceStatusConnected,
							LineProtocolStatus: arista.IPInterfaceLineProtocolStatusUp,
							InterfaceAddress: arista.IPInterfaceAddress{
								IPAddr: arista.IPInterfaceAddressIPAddr{
									Address: "10.1.1.0",
									MaskLen: 31,
								},
							},
						},
						"Tunnel1-3": {
							InterfaceStatus:    arista.IPInterfaceInterfaceStatusConnected,
							LineProtocolStatus: arista.IPInterfaceLineProtocolStatusUp,
							InterfaceAddress: arista.IPInterfaceAddress{
								IPAddr: arista.IPInterfaceAddressIPAddr{
									Address: "10.1.1.2",
									MaskLen: 31,
								},
							},
						},
						"Tunnel2-1": {
							InterfaceStatus:    arista.IPInterfaceInterfaceStatusConnected,
							LineProtocolStatus: arista.IPInterfaceLineProtocolStatusUp,
							InterfaceAddress: arista.IPInterfaceAddress{
								IPAddr: arista.IPInterfaceAddressIPAddr{
									Address: "10.1.1.5",
									MaskLen: 31,
								},
							},
						},
						"TunnelOther": {
							InterfaceStatus:    arista.IPInterfaceInterfaceStatusConnected,
							LineProtocolStatus: arista.IPInterfaceLineProtocolStatusUp,
							InterfaceAddress: arista.IPInterfaceAddress{
								IPAddr: arista.IPInterfaceAddressIPAddr{
									Address: "10.1.1.100",
									MaskLen: 31,
								},
							},
						},
					},
				}

				respJSON, err := json.Marshal(resp)
				require.NoError(t, err)

				return &aristapb.RunShowCmdResponse{
					Response: &aristapb.EapiResponse{
						Success:   true,
						Responses: []string{string(respJSON)},
					},
				}, nil
			},
		}

		config := &telemetry.LedgerPeerDiscoveryConfig{
			Logger:           log,
			LocalDevicePK:    localDevicePK,
			TWAMPPort:        12345,
			RefreshInterval:  100 * time.Millisecond,
			ProgramClient:    serviceabilityProgram,
			AristaEAPIClient: aristaEAPIClient,
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

		links := serviceabilityProgram.GetLinks()
		expected := map[string]*telemetry.Peer{
			solana.PublicKeyFromBytes(links[0].PubKey[:]).String(): {
				LinkPK:     stringToPubkey("link_1-2"),
				DevicePK:   stringToPubkey("device2"),
				DeviceAddr: &net.UDPAddr{IP: ipv4([4]uint8{10, 1, 1, 1}), Port: 12345},
			},
			solana.PublicKeyFromBytes(links[1].PubKey[:]).String(): {
				LinkPK:     stringToPubkey("link_1-3"),
				DevicePK:   stringToPubkey("device3"),
				DeviceAddr: &net.UDPAddr{IP: ipv4([4]uint8{10, 1, 1, 3}), Port: 12345},
			},
			solana.PublicKeyFromBytes(links[2].PubKey[:]).String(): {
				LinkPK:     stringToPubkey("link_2-1"),
				DevicePK:   stringToPubkey("device2"),
				DeviceAddr: &net.UDPAddr{IP: ipv4([4]uint8{10, 1, 1, 4}), Port: 12345},
			},
		}

		assert.Equal(t, expected, peers.GetPeers())
	})

	t.Run("skips pending links", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		localDevicePK := stringToPubkey("device1")

		serviceabilityProgram := newMockServiceabilityProgramClient(func(c *mockServiceabilityProgramClient) error {
			c.devices = []serviceability.Device{
				{PubKey: localDevicePK},
				{PubKey: stringToPubkey("device2")},
			}
			c.links = []serviceability.Link{
				{
					PubKey:      stringToPubkey("inactive_link"),
					Status:      serviceability.LinkStatusPending,
					SideAPubKey: localDevicePK,
					SideZPubKey: stringToPubkey("device2"),
					TunnelNet:   [5]uint8{10, 1, 2, 0, 31},
				},
			}
			return nil
		})

		aristaEAPIClient := &arista.MockEAPIClient{
			RunShowCmdFunc: func(ctx context.Context, req *aristapb.RunShowCmdRequest, opts ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
				resp := arista.IPInterfacesBriefResponse{
					Interfaces: map[string]arista.IPInterfaceBrief{
						"TunnelX": {
							InterfaceStatus:    arista.IPInterfaceInterfaceStatusConnected,
							LineProtocolStatus: arista.IPInterfaceLineProtocolStatusUp,
							InterfaceAddress: arista.IPInterfaceAddress{
								IPAddr: arista.IPInterfaceAddressIPAddr{
									Address: "10.1.2.0",
									MaskLen: 31,
								},
							},
						},
					},
				}
				j, _ := json.Marshal(resp)
				return &aristapb.RunShowCmdResponse{
					Response: &aristapb.EapiResponse{Success: true, Responses: []string{string(j)}},
				}, nil
			},
		}

		cfg := &telemetry.LedgerPeerDiscoveryConfig{
			Logger:           log,
			LocalDevicePK:    localDevicePK,
			ProgramClient:    serviceabilityProgram,
			AristaEAPIClient: aristaEAPIClient,
			TWAMPPort:        1234,
			RefreshInterval:  50 * time.Millisecond,
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

		serviceabilityProgram := newMockServiceabilityProgramClient(func(c *mockServiceabilityProgramClient) error {
			c.devices = []serviceability.Device{
				{PubKey: localDevicePK},
				{PubKey: stringToPubkey("device2")},
			}
			c.links = []serviceability.Link{
				{
					PubKey:      stringToPubkey("bad_tunnel_net"),
					Status:      serviceability.LinkStatusActivated,
					SideAPubKey: localDevicePK,
					SideZPubKey: stringToPubkey("device2"),
					TunnelNet:   [5]uint8{0, 0, 0, 0, 0}, // invalid
				},
			}
			return nil
		})

		aristaEAPIClient := &arista.MockEAPIClient{
			RunShowCmdFunc: func(ctx context.Context, req *aristapb.RunShowCmdRequest, opts ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
				resp := arista.IPInterfacesBriefResponse{
					Interfaces: map[string]arista.IPInterfaceBrief{
						"TunnelX": {
							InterfaceStatus:    arista.IPInterfaceInterfaceStatusConnected,
							LineProtocolStatus: arista.IPInterfaceLineProtocolStatusUp,
							InterfaceAddress: arista.IPInterfaceAddress{
								IPAddr: arista.IPInterfaceAddressIPAddr{
									Address: "10.2.2.0",
									MaskLen: 31,
								},
							},
						},
					},
				}
				j, _ := json.Marshal(resp)
				return &aristapb.RunShowCmdResponse{
					Response: &aristapb.EapiResponse{Success: true, Responses: []string{string(j)}},
				}, nil
			},
		}

		cfg := &telemetry.LedgerPeerDiscoveryConfig{
			Logger:           log,
			LocalDevicePK:    localDevicePK,
			ProgramClient:    serviceabilityProgram,
			AristaEAPIClient: aristaEAPIClient,
			TWAMPPort:        1234,
			RefreshInterval:  50 * time.Millisecond,
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
				{PubKey: linkPK, SideAPubKey: localDevicePK, SideZPubKey: device2PK},
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
			AristaEAPIClient: &arista.MockEAPIClient{
				RunShowCmdFunc: func(ctx context.Context, req *aristapb.RunShowCmdRequest, opts ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
					return nil, nil
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
		cfg.AristaEAPIClient = nil
		base(cfg, "nil arista EAPI client")

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
