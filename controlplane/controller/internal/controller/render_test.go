package controller

import (
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/malbeclabs/doublezero/controlplane/controller/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

func TestRenderConfig(t *testing.T) {
	tests := []struct {
		Name        string
		Description string
		Data        templateData
		Want        string
	}{
		{
			Name:        "render_unicast_tunnels_successfully",
			Description: "render config for a set of unicast tunnels",
			Data: templateData{
				Strings:                  StringsHelper{},
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				LocalASN:                 65342,
				UnicastVrfs:              []uint16{1},
				Device: &Device{
					PublicIP:              net.IP{7, 7, 7, 7},
					Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
					Vpn4vLoopbackIntfName: "Loopback255",
					Interfaces:            []Interface{},
					IsisNet:               "49.0000.0e0e.0e0e.0000.00",
					ExchangeCode:          "tst",
					BgpCommunity:          10050,
					Tunnels: []*Tunnel{
						{
							Id:            500,
							UnderlaySrcIP: net.IP{1, 1, 1, 1},
							UnderlayDstIP: net.IP{2, 2, 2, 2},
							OverlaySrcIP:  net.IP{169, 254, 0, 0},
							OverlayDstIP:  net.IP{169, 254, 0, 1},
							DzIp:          net.IP{100, 0, 0, 0},
							Allocated:     true,
							VrfId:         1,
						},
						{
							Id:            501,
							UnderlaySrcIP: net.IP{3, 3, 3, 3},
							UnderlayDstIP: net.IP{4, 4, 4, 4},
							OverlaySrcIP:  net.IP{169, 254, 0, 2},
							OverlayDstIP:  net.IP{169, 254, 0, 3},
							DzIp:          net.IP{100, 0, 0, 1},
							Allocated:     true,
							VrfId:         1,
						},
						{
							Id:            502,
							UnderlaySrcIP: net.IP{5, 5, 5, 5},
							UnderlayDstIP: net.IP{6, 6, 6, 6},
							OverlaySrcIP:  net.IP{169, 254, 0, 4},
							OverlayDstIP:  net.IP{169, 254, 0, 5},
							DzIp:          net.IP{100, 0, 0, 2},
							Allocated:     true,
							VrfId:         1,
						},
					},
				},
				UnknownBgpPeers: nil,
			},
			Want: "fixtures/unicast.tunnel.tmpl",
		},
		{
			Name:        "render_peer_removal_successful",
			Description: "render config for removal of unknown peers successfully",
			Data: templateData{
				Strings:                  StringsHelper{},
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				LocalASN:                 21682,
				UnicastVrfs:              []uint16{1},
				Device: &Device{
					Interfaces:            []Interface{},
					PublicIP:              net.IP{7, 7, 7, 7},
					Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
					Vpn4vLoopbackIntfName: "Loopback255",
					IsisNet:               "49.0000.0e0e.0e0e.0000.00",
					ExchangeCode:          "tst",
					BgpCommunity:          10050,
					Tunnels: []*Tunnel{
						{
							Id:            500,
							UnderlaySrcIP: net.IP{1, 1, 1, 1},
							UnderlayDstIP: net.IP{2, 2, 2, 2},
							OverlaySrcIP:  net.IP{169, 254, 0, 0},
							OverlayDstIP:  net.IP{169, 254, 0, 1},
							DzIp:          net.IP{100, 0, 0, 0},
							Allocated:     true,
							VrfId:         1,
						},
						{
							Id:            501,
							UnderlaySrcIP: net.IP{3, 3, 3, 3},
							UnderlayDstIP: net.IP{4, 4, 4, 4},
							OverlaySrcIP:  net.IP{169, 254, 0, 2},
							OverlayDstIP:  net.IP{169, 254, 0, 3},
							DzIp:          net.IP{100, 0, 0, 1},
							Allocated:     true,
							VrfId:         1,
						},
						{
							Id:            502,
							UnderlaySrcIP: net.IP{5, 5, 5, 5},
							UnderlayDstIP: net.IP{6, 6, 6, 6},
							OverlaySrcIP:  net.IP{169, 254, 0, 4},
							OverlayDstIP:  net.IP{169, 254, 0, 5},
							DzIp:          net.IP{100, 0, 0, 2},
							Allocated:     true,
							VrfId:         1,
						},
					},
				},
				UnknownBgpPeers: []net.IP{
					{169, 254, 0, 7},
				},
			},
			Want: "fixtures/unknown.peer.removal.tmpl",
		},
		{
			Name:        "render_multicast_tunnel_successfully",
			Description: "render config for a multicast tunnel",
			Data: templateData{
				Strings:                  StringsHelper{},
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				LocalASN:                 65342,
				UnicastVrfs:              []uint16{1},
				Device: &Device{
					Interfaces:            []Interface{},
					PublicIP:              net.IP{7, 7, 7, 7},
					Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
					Vpn4vLoopbackIntfName: "Loopback255",
					IsisNet:               "49.0000.0e0e.0e0e.0000.00",
					ExchangeCode:          "tst",
					BgpCommunity:          10050,
					Tunnels: []*Tunnel{
						{
							Id:            500,
							UnderlaySrcIP: net.IP{1, 1, 1, 1},
							UnderlayDstIP: net.IP{2, 2, 2, 2},
							OverlaySrcIP:  net.IP{169, 254, 0, 0},
							OverlayDstIP:  net.IP{169, 254, 0, 1},
							DzIp:          net.IP{100, 0, 0, 0},
							Allocated:     true,
							IsMulticast:   true,
							MulticastBoundaryList: []net.IP{
								{239, 0, 0, 1},
								{239, 0, 0, 2},
							},
							MulticastSubscribers: []net.IP{
								{239, 0, 0, 1},
								{239, 0, 0, 2},
							},
							MulticastPublishers: []net.IP{},
						},
						{
							Id:            501,
							UnderlaySrcIP: net.IP{3, 3, 3, 3},
							UnderlayDstIP: net.IP{4, 4, 4, 4},
							OverlaySrcIP:  net.IP{169, 254, 0, 2},
							OverlayDstIP:  net.IP{169, 254, 0, 3},
							DzIp:          net.IP{100, 0, 0, 1},
							Allocated:     true,
							IsMulticast:   true,
							MulticastBoundaryList: []net.IP{
								{239, 0, 0, 3},
								{239, 0, 0, 4},
							},
							MulticastSubscribers: []net.IP{},
							MulticastPublishers: []net.IP{
								{239, 0, 0, 3},
								{239, 0, 0, 4},
							},
						},
						{
							Id:            502,
							UnderlaySrcIP: net.IP{5, 5, 5, 5},
							UnderlayDstIP: net.IP{6, 6, 6, 6},
							OverlaySrcIP:  net.IP{169, 254, 0, 4},
							OverlayDstIP:  net.IP{169, 254, 0, 5},
							DzIp:          net.IP{100, 0, 0, 2},
							Allocated:     true,
							IsMulticast:   true,
							MulticastBoundaryList: []net.IP{
								{239, 0, 0, 5},
								{239, 0, 0, 6},
							},
							MulticastSubscribers: []net.IP{
								{239, 0, 0, 5},
								{239, 0, 0, 6},
							},
							MulticastPublishers: []net.IP{
								{239, 0, 0, 5},
								{239, 0, 0, 6},
							},
						},
					},
				},
				UnknownBgpPeers: []net.IP{},
			},
			Want: "fixtures/multicast.tunnel.tmpl",
		},
		{
			Name:        "render_mixed_tunnels_successfully",
			Description: "render config for a mix of unicast and multicast tunnels",
			Data: templateData{
				Strings:                  StringsHelper{},
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				LocalASN:                 65342,
				UnicastVrfs:              []uint16{1},
				Device: &Device{
					Interfaces:            []Interface{},
					PublicIP:              net.IP{7, 7, 7, 7},
					Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
					Vpn4vLoopbackIntfName: "Loopback255",
					IsisNet:               "49.0000.0e0e.0e0e.0000.00",
					ExchangeCode:          "tst",
					BgpCommunity:          10050,
					Tunnels: []*Tunnel{
						{
							Id:            500,
							UnderlaySrcIP: net.IP{1, 1, 1, 1},
							UnderlayDstIP: net.IP{2, 2, 2, 2},
							OverlaySrcIP:  net.IP{169, 254, 0, 0},
							OverlayDstIP:  net.IP{169, 254, 0, 1},
							DzIp:          net.IP{100, 0, 0, 0},
							Allocated:     true,
							IsMulticast:   true,
							MulticastBoundaryList: []net.IP{
								{239, 0, 0, 1},
								{239, 0, 0, 2},
							},
							MulticastSubscribers: []net.IP{
								{239, 0, 0, 1},
								{239, 0, 0, 2},
							},
							MulticastPublishers: []net.IP{},
						},
						{
							Id:            501,
							UnderlaySrcIP: net.IP{3, 3, 3, 3},
							UnderlayDstIP: net.IP{4, 4, 4, 4},
							OverlaySrcIP:  net.IP{169, 254, 0, 2},
							OverlayDstIP:  net.IP{169, 254, 0, 3},
							DzIp:          net.IP{100, 0, 0, 1},
							Allocated:     true,
							VrfId:         1,
						},
						{
							Id:            502,
							UnderlaySrcIP: net.IP{5, 5, 5, 5},
							UnderlayDstIP: net.IP{6, 6, 6, 6},
							OverlaySrcIP:  net.IP{169, 254, 0, 4},
							OverlayDstIP:  net.IP{169, 254, 0, 5},
							DzIp:          net.IP{100, 0, 0, 2},
							Allocated:     true,
							IsMulticast:   true,
							MulticastBoundaryList: []net.IP{
								{239, 0, 0, 3},
								{239, 0, 0, 4},
								{239, 0, 0, 5},
								{239, 0, 0, 6},
							},
							MulticastSubscribers: []net.IP{
								{239, 0, 0, 3},
								{239, 0, 0, 4},
							},
							MulticastPublishers: []net.IP{
								{239, 0, 0, 5},
								{239, 0, 0, 6},
							},
						},
						{
							Id:            503,
							UnderlaySrcIP: net.IP{7, 7, 7, 7},
							UnderlayDstIP: net.IP{8, 8, 8, 8},
							OverlaySrcIP:  net.IP{169, 254, 0, 6},
							OverlayDstIP:  net.IP{169, 254, 0, 7},
							DzIp:          net.IP{100, 0, 0, 3},
							Allocated:     true,
							IsMulticast:   true,
							MulticastBoundaryList: []net.IP{
								{239, 0, 0, 5},
								{239, 0, 0, 6},
							},
							MulticastSubscribers: []net.IP{},
							MulticastPublishers: []net.IP{
								{239, 0, 0, 5},
								{239, 0, 0, 6},
							},
						},
					},
				},
				UnknownBgpPeers: []net.IP{},
			},
			Want: "fixtures/mixed.tunnel.tmpl",
		},
		{
			Name:        "render_nohardware_tunnels_successfully",
			Description: "render config for a mix of unicast and multicast tunnels with no hardware option",
			Data: templateData{
				Strings:                  StringsHelper{},
				NoHardware:               true,
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				LocalASN:                 65342,
				UnicastVrfs:              []uint16{1},
				Device: &Device{
					Interfaces:            []Interface{},
					PublicIP:              net.IP{7, 7, 7, 7},
					Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
					Vpn4vLoopbackIntfName: "Loopback255",
					IsisNet:               "49.0000.0e0e.0e0e.0000.00",
					ExchangeCode:          "tst",
					BgpCommunity:          10050,
					Tunnels: []*Tunnel{
						{
							Id:            500,
							UnderlaySrcIP: net.IP{1, 1, 1, 1},
							UnderlayDstIP: net.IP{2, 2, 2, 2},
							OverlaySrcIP:  net.IP{169, 254, 0, 0},
							OverlayDstIP:  net.IP{169, 254, 0, 1},
							DzIp:          net.IP{100, 0, 0, 0},
							Allocated:     true,
							IsMulticast:   true,
							MulticastBoundaryList: []net.IP{
								{239, 0, 0, 1},
								{239, 0, 0, 2},
							},
							MulticastSubscribers: []net.IP{
								{239, 0, 0, 1},
								{239, 0, 0, 2},
							},
							MulticastPublishers: []net.IP{},
						},
						{
							Id:            501,
							UnderlaySrcIP: net.IP{3, 3, 3, 3},
							UnderlayDstIP: net.IP{4, 4, 4, 4},
							OverlaySrcIP:  net.IP{169, 254, 0, 2},
							OverlayDstIP:  net.IP{169, 254, 0, 3},
							DzIp:          net.IP{100, 0, 0, 1},
							Allocated:     true,
							VrfId:         1,
						},
						{
							Id:            502,
							UnderlaySrcIP: net.IP{5, 5, 5, 5},
							UnderlayDstIP: net.IP{6, 6, 6, 6},
							OverlaySrcIP:  net.IP{169, 254, 0, 4},
							OverlayDstIP:  net.IP{169, 254, 0, 5},
							DzIp:          net.IP{100, 0, 0, 2},
							Allocated:     true,
							IsMulticast:   true,
							MulticastBoundaryList: []net.IP{
								{239, 0, 0, 3},
								{239, 0, 0, 4},
								{239, 0, 0, 5},
								{239, 0, 0, 6},
							},
							MulticastSubscribers: []net.IP{
								{239, 0, 0, 3},
								{239, 0, 0, 4},
							},
							MulticastPublishers: []net.IP{
								{239, 0, 0, 5},
								{239, 0, 0, 6},
							},
						},
						{
							Id:            503,
							UnderlaySrcIP: net.IP{7, 7, 7, 7},
							UnderlayDstIP: net.IP{8, 8, 8, 8},
							OverlaySrcIP:  net.IP{169, 254, 0, 6},
							OverlayDstIP:  net.IP{169, 254, 0, 7},
							DzIp:          net.IP{100, 0, 0, 3},
							Allocated:     true,
							IsMulticast:   true,
							MulticastBoundaryList: []net.IP{
								{239, 0, 0, 5},
								{239, 0, 0, 6},
							},
							MulticastSubscribers: []net.IP{},
							MulticastPublishers: []net.IP{
								{239, 0, 0, 5},
								{239, 0, 0, 6},
							},
						},
					},
				},
				UnknownBgpPeers: []net.IP{},
			},
			Want: "fixtures/nohardware.tunnel.tmpl",
		},
		{
			Name:        "render_interfaces_successfully",
			Description: "render config for a set of interfaces",
			Data: templateData{
				Strings:                  StringsHelper{},
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				LocalASN:                 65342,
				UnicastVrfs:              []uint16{1},
				Device: &Device{
					PublicIP:              net.IP{7, 7, 7, 7},
					Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
					Vpn4vLoopbackIntfName: "Loopback255",
					IsisNet:               "49.0000.0e0e.0e0e.0000.00",
					Ipv4LoopbackIP:        net.IP{13, 13, 13, 13},
					ExchangeCode:          "tst",
					BgpCommunity:          10050,
					Interfaces: []Interface{
						{
							Name:           "Loopback255",
							Ip:             netip.MustParsePrefix("172.31.1.255/32"),
							NodeSegmentIdx: 101,
							InterfaceType:  InterfaceTypeLoopback,
							LoopbackType:   LoopbackTypeVpnv4,
						},
						{
							Name:          "Loopback256",
							Ip:            netip.MustParsePrefix("172.29.1.255/32"),
							InterfaceType: InterfaceTypeLoopback,
							LoopbackType:  LoopbackTypeIpv4,
						},
						{
							Name:          "Switch1/1/1",
							Ip:            netip.MustParsePrefix("172.16.0.0/31"),
							InterfaceType: InterfaceTypePhysical,
							Metric:        40000,
							IsLink:        true,
							LinkStatus:    serviceability.LinkStatusActivated,
						},
						{
							Name:                 "Switch1/1/2",
							IsSubInterfaceParent: true,
							InterfaceType:        InterfaceTypePhysical,
						},
						{
							Name:           "Switch1/1/2.100",
							Ip:             netip.MustParsePrefix("172.16.0.2/31"),
							VlanId:         100,
							IsSubInterface: true,
							InterfaceType:  InterfaceTypePhysical,
							Metric:         0,
							IsLink:         true, // No metric w/ IsLink true should not render isis config
						},
						{
							Name:           "Switch1/1/2.200",
							Ip:             netip.MustParsePrefix("172.16.0.6/31"),
							VlanId:         200,
							IsSubInterface: true,
							InterfaceType:  InterfaceTypePhysical,
							Metric:         40000, // Metric w/ IsLink false should not render isis config
							IsLink:         false,
						},
						{
							Name:           "Switch1/1/2.300",
							Ip:             netip.MustParsePrefix("172.16.0.8/31"),
							VlanId:         300,
							IsSubInterface: true,
							InterfaceType:  InterfaceTypePhysical,
							Metric:         40000,
							IsLink:         true,
							LinkStatus:     serviceability.LinkStatusActivated,
						},
						{
							Name:          "Vlan4001",
							Ip:            netip.MustParsePrefix("172.16.0.4/31"),
							InterfaceType: InterfaceTypePhysical,
							Metric:        10000,
							IsLink:        true,
							LinkStatus:    serviceability.LinkStatusActivated,
						},
						{
							Name:          "Switch1/1/3",
							Ip:            netip.MustParsePrefix("172.16.0.10/31"),
							InterfaceType: InterfaceTypePhysical,
							Metric:        1000000,
							IsLink:        true,
							LinkStatus:    serviceability.LinkStatusSoftDrained,
						},
						{
							Name:          "Switch1/1/4",
							Ip:            netip.MustParsePrefix("172.16.0.12/31"),
							InterfaceType: InterfaceTypePhysical,
							Metric:        30000,
							IsLink:        true,
							LinkStatus:    serviceability.LinkStatusHardDrained,
						},
					},
				},
				UnknownBgpPeers: []net.IP{},
			},
			Want: "fixtures/interfaces.txt",
		},
		{
			Name:        "render_base_config_successfully",
			Description: "render base device config without tunnels",
			Data: templateData{
				Strings:                  StringsHelper{},
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				LocalASN:                 65342,
				UnicastVrfs:              []uint16{1},
				Device: &Device{
					PublicIP:        net.IP{7, 7, 7, 7},
					Vpn4vLoopbackIP: net.IP{14, 14, 14, 14},
					IsisNet:         "49.0000.0e0e.0e0e.0000.00",
					Ipv4LoopbackIP:  net.IP{13, 13, 13, 13},
					ExchangeCode:    "tst",
					BgpCommunity:    10050,
					Interfaces: []Interface{
						{
							Name:           "Loopback255",
							InterfaceType:  InterfaceTypeLoopback,
							LoopbackType:   LoopbackTypeVpnv4,
							Ip:             netip.MustParsePrefix("14.14.14.14/32"),
							NodeSegmentIdx: 15,
						},
						{
							Name:          "Ethernet1/1",
							InterfaceType: InterfaceTypePhysical,
							Ip:            netip.MustParsePrefix("172.16.0.2/31"),
							Metric:        40000,
							IsLink:        true,
						},
						{
							Name:          "Ethernet1/2",
							InterfaceType: InterfaceTypePhysical,
							Ip:            netip.MustParsePrefix("172.16.0.4/31"),
						},
					},
					Vpn4vLoopbackIntfName: "Loopback255",
					Ipv4LoopbackIntfName:  "Loopback256",
				},
				Vpnv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{15, 15, 15, 15},
						PeerName: "remote-dzd",
					},
				},
				Ipv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{12, 12, 12, 12},
						PeerName: "remote-dzd",
					},
				},
			},
			Want: "fixtures/base.config.txt",
		},
		{
			Name:        "render_drained_device_config_successfully",
			Description: "render config for a drained device with BGP, MSDP, and ISIS shutdown",
			Data: templateData{
				Strings:                  StringsHelper{},
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				LocalASN:                 65342,
				UnicastVrfs:              []uint16{1},
				Device: &Device{
					PublicIP:        net.IP{7, 7, 7, 7},
					Vpn4vLoopbackIP: net.IP{14, 14, 14, 14},
					IsisNet:         "49.0000.0e0e.0e0e.0000.00",
					Ipv4LoopbackIP:  net.IP{13, 13, 13, 13},
					ExchangeCode:    "tst",
					BgpCommunity:    10050,
					Status:          serviceability.DeviceStatusDrained,
					Interfaces: []Interface{
						{
							Name:           "Loopback255",
							InterfaceType:  InterfaceTypeLoopback,
							LoopbackType:   LoopbackTypeVpnv4,
							Ip:             netip.MustParsePrefix("14.14.14.14/32"),
							NodeSegmentIdx: 15,
						},
						{
							Name:          "Ethernet1/1",
							InterfaceType: InterfaceTypePhysical,
							Ip:            netip.MustParsePrefix("172.16.0.2/31"),
							Metric:        40000,
							IsLink:        true,
						},
						{
							Name:          "Ethernet1/2",
							InterfaceType: InterfaceTypePhysical,
							Ip:            netip.MustParsePrefix("172.16.0.4/31"),
						},
					},
					Vpn4vLoopbackIntfName: "Loopback255",
					Ipv4LoopbackIntfName:  "Loopback256",
				},
				Vpnv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{15, 15, 15, 15},
						PeerName: "remote-dzd",
					},
				},
				Ipv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{12, 12, 12, 12},
						PeerName: "remote-dzd",
					},
				},
			},
			Want: "fixtures/base.config.drained.txt",
		},
		{
			Name:        "render_multi_vrf_tunnels_successfully",
			Description: "render config for unicast tunnels across multiple VRFs",
			Data: templateData{
				Strings:                  StringsHelper{},
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				LocalASN:                 65342,
				UnicastVrfs:              []uint16{1, 2},
				Device: &Device{
					PublicIP:              net.IP{7, 7, 7, 7},
					Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
					Vpn4vLoopbackIntfName: "Loopback255",
					Interfaces:            []Interface{},
					IsisNet:               "49.0000.0e0e.0e0e.0000.00",
					ExchangeCode:          "tst",
					BgpCommunity:          10050,
					Tunnels: []*Tunnel{
						{
							Id:            500,
							UnderlaySrcIP: net.IP{1, 1, 1, 1},
							UnderlayDstIP: net.IP{2, 2, 2, 2},
							OverlaySrcIP:  net.IP{169, 254, 0, 0},
							OverlayDstIP:  net.IP{169, 254, 0, 1},
							DzIp:          net.IP{100, 0, 0, 0},
							Allocated:     true,
							VrfId:         1,
						},
						{
							Id:            501,
							UnderlaySrcIP: net.IP{3, 3, 3, 3},
							UnderlayDstIP: net.IP{4, 4, 4, 4},
							OverlaySrcIP:  net.IP{169, 254, 0, 2},
							OverlayDstIP:  net.IP{169, 254, 0, 3},
							DzIp:          net.IP{100, 0, 0, 1},
							Allocated:     true,
							VrfId:         2,
						},
					},
				},
				UnknownBgpPeers: nil,
			},
			Want: "fixtures/multi.vrf.tunnel.tmpl",
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			got, err := renderConfig(test.Data)
			if err != nil {
				t.Fatalf("error rendering template: %v", err)
			}
			var want []byte
			if strings.HasSuffix(test.Want, ".tmpl") {
				templateData := map[string]int{
					"StartTunnel": config.StartUserTunnelNum,
					"EndTunnel":   config.StartUserTunnelNum + config.MaxUserTunnelSlots - 1,
				}
				rendered, err := renderTemplateFile(test.Want, templateData)
				if err != nil {
					t.Fatalf("error rendering test fixture %s: %v", test.Want, err)
				}
				want = []byte(rendered)
			} else {
				var err error
				want, err = os.ReadFile(test.Want)
				if err != nil {
					t.Fatalf("error reading test fixture %s: %v", test.Want, err)
				}
			}
			if diff := cmp.Diff(string(want), got); diff != "" {
				t.Errorf("renderTunnels mismatch in fixture %s (-want +got):\n%s", test.Want, diff)
			}
		})
	}
}
