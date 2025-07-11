package controller

import (
	"net"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
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
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				Device: &Device{
					PublicIP: net.IP{7, 7, 7, 7},
					Tunnels: []*Tunnel{
						{
							Id:            500,
							UnderlaySrcIP: net.IP{1, 1, 1, 1},
							UnderlayDstIP: net.IP{2, 2, 2, 2},
							OverlaySrcIP:  net.IP{169, 254, 0, 0},
							OverlayDstIP:  net.IP{169, 254, 0, 1},
							DzIp:          net.IP{100, 0, 0, 0},
							Allocated:     true,
						},
						{
							Id:            501,
							UnderlaySrcIP: net.IP{3, 3, 3, 3},
							UnderlayDstIP: net.IP{4, 4, 4, 4},
							OverlaySrcIP:  net.IP{169, 254, 0, 2},
							OverlayDstIP:  net.IP{169, 254, 0, 3},
							DzIp:          net.IP{100, 0, 0, 1},
							Allocated:     true,
						},
						{
							Id:            502,
							UnderlaySrcIP: net.IP{5, 5, 5, 5},
							UnderlayDstIP: net.IP{6, 6, 6, 6},
							OverlaySrcIP:  net.IP{169, 254, 0, 4},
							OverlayDstIP:  net.IP{169, 254, 0, 5},
							DzIp:          net.IP{100, 0, 0, 2},
							Allocated:     true,
						},
					},
				},
				UnknownBgpPeers: []net.IP{},
			},
			Want: "fixtures/unicast.tunnel.txt",
		},
		{
			Name:        "render_peer_removal_successfully",
			Description: "render config for removal of unknown peers successfully",
			Data: templateData{
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				Device: &Device{
					PublicIP: net.IP{7, 7, 7, 7},
					Tunnels: []*Tunnel{
						{
							Id:            500,
							UnderlaySrcIP: net.IP{1, 1, 1, 1},
							UnderlayDstIP: net.IP{2, 2, 2, 2},
							OverlaySrcIP:  net.IP{169, 254, 0, 0},
							OverlayDstIP:  net.IP{169, 254, 0, 1},
							DzIp:          net.IP{100, 0, 0, 0},
							Allocated:     true,
						},
						{
							Id:            501,
							UnderlaySrcIP: net.IP{3, 3, 3, 3},
							UnderlayDstIP: net.IP{4, 4, 4, 4},
							OverlaySrcIP:  net.IP{169, 254, 0, 2},
							OverlayDstIP:  net.IP{169, 254, 0, 3},
							DzIp:          net.IP{100, 0, 0, 1},
							Allocated:     true,
						},
						{
							Id:            502,
							UnderlaySrcIP: net.IP{5, 5, 5, 5},
							UnderlayDstIP: net.IP{6, 6, 6, 6},
							OverlaySrcIP:  net.IP{169, 254, 0, 4},
							OverlayDstIP:  net.IP{169, 254, 0, 5},
							DzIp:          net.IP{100, 0, 0, 2},
							Allocated:     true,
						},
					},
				},
				UnknownBgpPeers: []net.IP{
					{169, 254, 0, 7},
				},
			},
			Want: "fixtures/unknown.peer.removal.txt",
		},
		{
			Name:        "render_multicast_tunnel_successfully",
			Description: "render config for a multicast tunnel",
			Data: templateData{
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				Device: &Device{
					PublicIP: net.IP{7, 7, 7, 7},
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
			Want: "fixtures/multicast.tunnel.txt",
		},
		{
			Name:        "render_mixed_tunnels_successfully",
			Description: "render config for a mix of unicast and multicast tunnels",
			Data: templateData{
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				Device: &Device{
					PublicIP: net.IP{7, 7, 7, 7},
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
			Want: "fixtures/mixed.tunnel.txt",
		},
		{
			Name:        "render_nohardware_tunnels_successfully",
			Description: "render config for a mix of unicast and multicast tunnels with no hardware option",
			Data: templateData{
				NoHardware:               true,
				MulticastGroupBlock:      "239.0.0.0/24",
				TelemetryTWAMPListenPort: 862,
				Device: &Device{
					PublicIP: net.IP{7, 7, 7, 7},
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
			Want: "fixtures/nohardware.tunnel.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			got, err := renderConfig(test.Data)
			if err != nil {
				t.Fatalf("error rendering template: %v", err)
			}
			want, err := os.ReadFile(test.Want)
			if err != nil {
				t.Fatalf("error reading test fixture %s: %v", test.Want, err)
			}
			if diff := cmp.Diff(string(want), got); diff != "" {
				t.Errorf("renderTunnels mismatch (-want +got): %s\n", diff)
			}
		})
	}
}
