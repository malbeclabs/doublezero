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
							MetroRouting:  true,
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
							MetroRouting:  true,
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
							MetroRouting:  true,
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
							MetroRouting:  true,
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
							MetroRouting:  true,
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
							MetroRouting:  true,
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
							MetroRouting:  true,
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
							MetroRouting:  true,
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
							Mtu:           2048,
							InterfaceType: InterfaceTypePhysical,
							Metric:        40000,
							IsLink:        true,
							LinkStatus:    serviceability.LinkStatusActivated,
						},
						{
							Name:                 "Switch1/1/2",
							Mtu:                  2048,
							IsSubInterfaceParent: true,
							InterfaceType:        InterfaceTypePhysical,
						},
						{
							Name:           "Switch1/1/2.100",
							Ip:             netip.MustParsePrefix("172.16.0.2/31"),
							VlanId:         100,
							Mtu:            2048,
							IsSubInterface: true,
							InterfaceType:  InterfaceTypePhysical,
							Metric:         0,
							IsLink:         true, // No metric w/ IsLink true should not render isis config
						},
						{
							Name:           "Switch1/1/2.200",
							Ip:             netip.MustParsePrefix("172.16.0.6/31"),
							VlanId:         200,
							Mtu:            2048,
							IsSubInterface: true,
							InterfaceType:  InterfaceTypePhysical,
							Metric:         40000, // Metric w/ IsLink false should not render isis config
							IsLink:         false,
						},
						{
							Name:           "Switch1/1/2.300",
							Ip:             netip.MustParsePrefix("172.16.0.8/31"),
							VlanId:         300,
							Mtu:            2048,
							IsSubInterface: true,
							InterfaceType:  InterfaceTypePhysical,
							Metric:         40000,
							IsLink:         true,
							LinkStatus:     serviceability.LinkStatusActivated,
						},
						{
							Name:          "Vlan4001",
							Ip:            netip.MustParsePrefix("172.16.0.4/31"),
							Mtu:           2048,
							InterfaceType: InterfaceTypePhysical,
							Metric:        10000,
							IsLink:        true,
							LinkStatus:    serviceability.LinkStatusActivated,
						},
						{
							Name:          "Switch1/1/3",
							Ip:            netip.MustParsePrefix("172.16.0.10/31"),
							Mtu:           2048,
							InterfaceType: InterfaceTypePhysical,
							Metric:        1000000,
							IsLink:        true,
							LinkStatus:    serviceability.LinkStatusSoftDrained,
						},
						{
							Name:          "Switch1/1/4",
							Ip:            netip.MustParsePrefix("172.16.0.12/31"),
							Mtu:           2048,
							InterfaceType: InterfaceTypePhysical,
							Metric:        30000,
							IsLink:        true,
							LinkStatus:    serviceability.LinkStatusHardDrained,
						},
						{
							Name:          "Switch1/1/5",
							Ip:            netip.MustParsePrefix("172.16.0.14/31"),
							Mtu:           2048,
							InterfaceType: InterfaceTypePhysical,
							Metric:        20000,
							IsLink:        true,
							LinkStatus:    serviceability.LinkStatusActivated,
							IsCYOA:        true,
						},
						{
							Name:          "Switch1/1/6",
							Ip:            netip.MustParsePrefix("172.16.0.16/31"),
							Mtu:           2048,
							InterfaceType: InterfaceTypePhysical,
							Metric:        25000,
							IsLink:        true,
							LinkStatus:    serviceability.LinkStatusActivated,
							IsDIA:         true,
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
							Mtu:           2048,
							Metric:        40000,
							IsLink:        true,
						},
						{
							Name:          "Ethernet1/2",
							InterfaceType: InterfaceTypePhysical,
							Ip:            netip.MustParsePrefix("172.16.0.4/31"),
							Mtu:           2048,
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
							Mtu:           2048,
							Metric:        40000,
							IsLink:        true,
						},
						{
							Name:          "Ethernet1/2",
							InterfaceType: InterfaceTypePhysical,
							Ip:            netip.MustParsePrefix("172.16.0.4/31"),
							Mtu:           2048,
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
							MetroRouting:  true,
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
							MetroRouting:  true,
						},
					},
				},
				UnknownBgpPeers: nil,
			},
			Want: "fixtures/multi.vrf.tunnel.tmpl",
		},
		{
			Name:        "render_metro_routing_disabled_tunnels_successfully",
			Description: "render config for unicast tunnels with metro routing disabled",
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
							MetroRouting:  false,
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
							MetroRouting:  false,
						},
					},
				},
				UnknownBgpPeers: nil,
			},
			Want: "fixtures/metro.routing.disabled.tunnel.tmpl",
		},
		{
			Name:        "render_multi_vrf_mixed_metro_routing_successfully",
			Description: "render config for unicast tunnels across VRFs with mixed metro routing settings",
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
							MetroRouting:  true,
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
							MetroRouting:  false,
						},
					},
				},
				UnknownBgpPeers: nil,
			},
			Want: "fixtures/multi.vrf.mixed.metro.routing.tunnel.tmpl",
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

func TestRenderFlexAlgoEnabled(t *testing.T) {
	cfg := &FeaturesConfig{}
	cfg.Features.FlexAlgo.Enabled = true
	cfg.Features.FlexAlgo.CommunityStamping.All = true

	data := templateData{
		Strings:                  StringsHelper{},
		MulticastGroupBlock:      "239.0.0.0/24",
		TelemetryTWAMPListenPort: 862,
		LocalASN:                 65342,
		UnicastVrfs:              []uint16{1},
		Config:                   cfg,
		AllTopologies: []TopologyModel{
			{
				Name:           "unicast-default",
				AdminGroupBit:  0,
				FlexAlgoNumber: 128,
				Color:          1,
				ConstraintStr:  "include-any",
			},
		},
		Device: &Device{
			PubKey:                "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
			PublicIP:              net.IP{7, 7, 7, 7},
			Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
			Vpn4vLoopbackIntfName: "Loopback255",
			IsisNet:               "49.0000.0e0e.0e0e.0000.00",
			ExchangeCode:          "tst",
			BgpCommunity:          10050,
			Interfaces: []Interface{
				{
					Name:           "Ethernet1/1",
					Ip:             netip.MustParsePrefix("172.16.0.2/31"),
					Mtu:            2048,
					InterfaceType:  InterfaceTypePhysical,
					Metric:         40000,
					IsLink:         true,
					LinkStatus:     serviceability.LinkStatusActivated,
					LinkTopologies: []string{"unicast-default"},
				},
				{
					Name:          "Loopback255",
					Ip:            netip.MustParsePrefix("14.14.14.14/32"),
					NodeSegmentIdx: 100,
					InterfaceType: InterfaceTypeLoopback,
					LoopbackType:  LoopbackTypeVpnv4,
					FlexAlgoNodeSegments: []FlexAlgoNodeSegmentModel{
						{NodeSegmentIdx: 200, TopologyName: "unicast-default"},
					},
				},
			},
			Tunnels: []*Tunnel{
				{
					Id:                   500,
					UnderlaySrcIP:        net.IP{1, 1, 1, 1},
					UnderlayDstIP:        net.IP{2, 2, 2, 2},
					OverlaySrcIP:         net.IP{169, 254, 0, 0},
					OverlayDstIP:         net.IP{169, 254, 0, 1},
					DzIp:                 net.IP{100, 0, 0, 0},
					Allocated:            true,
					VrfId:                1,
					MetroRouting:         true,
					TenantPubKey:         "g35TxFqwMx95vCk63fTxGTHb6ei4W24qg5t2x6xD3cT",
					TenantTopologyColors: "color 1",
				},
			},
		},
	}

	got, err := renderConfig(data)
	if err != nil {
		t.Fatalf("error rendering template: %v", err)
	}

	checks := []struct {
		desc    string
		present bool
		substr  string
	}{
		// Change 1: interface admin-group (uppercase)
		{"traffic-engineering enable on interface", true, "   traffic-engineering\n   traffic-engineering administrative-group UNICAST-DEFAULT"},
		// Change 3: BGP next-hop
		{"next-hop resolution ribs in vpn-ipv4", true, "next-hop resolution ribs tunnel-rib colored system-colored-tunnel-rib"},
		// Change 4: IS-IS flex-algo advertisement inside segment-routing mpls
		{"IS-IS flex-algo advertisement", true, "flex-algo unicast-default level-2 advertised"},
		{"IS-IS traffic-engineering block", true, "   traffic-engineering\n      no shutdown\n      is-type level-2"},
		// Change 5: router traffic-engineering block (correct structure)
		{"router traffic-engineering block", true, "router traffic-engineering"},
		{"router-id in TE block", true, "   router-id ipv4 14.14.14.14"},
		{"UNICAST-DRAINED alias first", true, "   administrative-group alias UNICAST-DRAINED group 1"},
		{"UNICAST-DEFAULT alias uppercase", true, "   administrative-group alias UNICAST-DEFAULT group 0"},
		{"flex-algo TE definition", true, "      flex-algo 128 unicast-default"},
		{"flex-algo admin-group include-any", true, "         administrative-group include any 0 exclude 1"},
		{"flex-algo color", true, "         color 1"},
		// Community stamping
		{"extcommunity color in route-map", true, "set extcommunity color color 1"},
		// Change 2: loopback node-segments
		{"loopback base node-segment", true, "   node-segment ipv4 index 100"},
		{"loopback flex-algo node-segment", true, "   node-segment ipv4 index 200 flex-algo unicast-default"},
		// Negative: wrong patterns must not appear
		{"no metric-type igp", false, "metric-type igp"},
		{"no topology standard", false, "topology standard"},
	}

	for _, c := range checks {
		t.Run(c.desc, func(t *testing.T) {
			if strings.Contains(got, c.substr) != c.present {
				if c.present {
					t.Errorf("expected %q to be present in rendered config, but it was not", c.substr)
				} else {
					t.Errorf("expected %q to be absent from rendered config, but it was present", c.substr)
				}
			}
		})
	}
}

func TestRenderFlexAlgoDisabled(t *testing.T) {
	// Config is nil — flex-algo blocks must not appear.
	data := templateData{
		Strings:                  StringsHelper{},
		MulticastGroupBlock:      "239.0.0.0/24",
		TelemetryTWAMPListenPort: 862,
		LocalASN:                 65342,
		UnicastVrfs:              []uint16{1},
		Config:                   nil,
		AllTopologies: []TopologyModel{
			{
				Name:           "unicast-default",
				AdminGroupBit:  0,
				FlexAlgoNumber: 128,
				Color:          1,
				ConstraintStr:  "include-any",
			},
		},
		Device: &Device{
			PublicIP:              net.IP{7, 7, 7, 7},
			Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
			Vpn4vLoopbackIntfName: "Loopback255",
			IsisNet:               "49.0000.0e0e.0e0e.0000.00",
			ExchangeCode:          "tst",
			BgpCommunity:          10050,
			Interfaces: []Interface{
				{
					Name:           "Ethernet1/1",
					Ip:             netip.MustParsePrefix("172.16.0.2/31"),
					Mtu:            2048,
					InterfaceType:  InterfaceTypePhysical,
					Metric:         40000,
					IsLink:         true,
					LinkStatus:     serviceability.LinkStatusActivated,
					LinkTopologies: []string{"unicast-default"},
				},
			},
			Tunnels: []*Tunnel{
				{
					Id:                   500,
					UnderlaySrcIP:        net.IP{1, 1, 1, 1},
					UnderlayDstIP:        net.IP{2, 2, 2, 2},
					OverlaySrcIP:         net.IP{169, 254, 0, 0},
					OverlayDstIP:         net.IP{169, 254, 0, 1},
					DzIp:                 net.IP{100, 0, 0, 0},
					Allocated:            true,
					VrfId:                1,
					MetroRouting:         true,
					TenantPubKey:         "g35TxFqwMx95vCk63fTxGTHb6ei4W24qg5t2x6xD3cT",
					TenantTopologyColors: "color 1",
				},
			},
		},
	}

	got, err := renderConfig(data)
	if err != nil {
		t.Fatalf("error rendering template: %v", err)
	}

	absent := []string{
		"traffic-engineering administrative-group",
		"router traffic-engineering",
		"administrative-group alias",
		"next-hop resolution ribs",
		"set extcommunity color",
	}

	for _, substr := range absent {
		t.Run("absent: "+substr, func(t *testing.T) {
			if strings.Contains(got, substr) {
				t.Errorf("expected %q to be absent from rendered config (flex-algo disabled), but it was present", substr)
			}
		})
	}
}
