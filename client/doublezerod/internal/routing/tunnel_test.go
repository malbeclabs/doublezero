package routing_test

import (
	"net"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

func TestTunnel_NewTunnel(t *testing.T) {
	tests := []struct {
		Name           string
		Description    string
		TunnelName     string
		LocalUnderlay  net.IP
		RemoteUnderlay net.IP
		OverlayPrefix  string
		ExpectError    bool
		Want           *routing.Tunnel
	}{
		{
			Name:           "valid_tunnel_happy_path",
			Description:    "create a valid tunnel",
			TunnelName:     "doublezero0",
			LocalUnderlay:  net.IPv4(1, 1, 1, 1),
			RemoteUnderlay: net.IPv4(2, 2, 2, 2),
			OverlayPrefix:  "10.1.1.0/31",
			ExpectError:    false,
			Want: &routing.Tunnel{
				Name:           "doublezero0",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(10, 1, 1, 1),
				RemoteOverlay:  net.IPv4(10, 1, 1, 0),
				MTU:            routing.GREMTU,
			},
		},
		{
			Name:           "wrong_overlay_prefix_length",
			Description:    "the tunnel p2p should always be a /31",
			TunnelName:     "doublezero0",
			LocalUnderlay:  net.IPv4(1, 1, 1, 1),
			RemoteUnderlay: net.IPv4(2, 2, 2, 2),
			OverlayPrefix:  "10.1.1.0/30",
			ExpectError:    true,
			Want:           nil,
		},
		{
			Name:           "invalid_overlay_prefix",
			Description:    "the tunnel p2p needs to be a valid cidr prefix",
			TunnelName:     "doublezero0",
			LocalUnderlay:  net.IPv4(1, 1, 1, 1),
			RemoteUnderlay: net.IPv4(2, 2, 2, 2),
			OverlayPrefix:  "10.1.1.0.0.0/30",
			ExpectError:    true,
			Want:           nil,
		},
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			got, err := routing.NewTunnel(test.TunnelName, test.LocalUnderlay, test.RemoteUnderlay, test.OverlayPrefix)
			if err != nil && !test.ExpectError {
				t.Errorf("error: %v", err)
			}
			if err == nil && test.ExpectError {
				t.Errorf("wanted error but returned nil")
			} else {
				if diff := cmp.Diff(test.Want, got); diff != "" {
					t.Errorf("Tunnel mismatch (-want +got): %s\n", diff)
				}
			}
		})
	}
}
