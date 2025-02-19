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
		Device      *Device
		Want        string
	}{
		{
			Name:        "render_tunnels_successfully",
			Description: "render config for a set of tunnels",
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
			Want: "fixtures/tunnel.txt",
		},
		{
			Name:        "render_peer_removal_successfully",
			Description: "render config for removal of unknown peers successfully",
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
				UnknownBgpPeers: []net.IP{
					{169, 254, 0, 7},
				},
			},
			Want: "fixtures/unknown.peer.removal.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			got, err := renderConfig(test.Device)
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
