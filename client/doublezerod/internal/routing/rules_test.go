package routing_test

import (
	"net"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

func TestRule_NewRule(t *testing.T) {
	tests := []struct {
		Name        string
		Description string
		ExpectError bool
		Priority    int
		Table       int
		Src         string
		Dst         string
		Want        *routing.IPRule
	}{
		{
			Name:        "return_rule_happy_path",
			Description: "generate an IP rule",
			ExpectError: false,
			Priority:    100,
			Table:       100,
			Src:         "100.0.0.0/24",
			Dst:         "100.0.0.0/24",
			Want: &routing.IPRule{
				Priority: 100,
				Table:    100,
				SrcNet:   &net.IPNet{IP: net.IPv4(100, 0, 0, 0), Mask: []byte{255, 255, 255, 0}},
				DstNet:   &net.IPNet{IP: net.IPv4(100, 0, 0, 0), Mask: []byte{255, 255, 255, 0}},
			},
		},
		{
			Name:        "table_out_of_range",
			Description: "table value must be within 100-200 range",
			ExpectError: true,
			Priority:    100,
			Table:       400,
			Src:         "100.0.0.0/24",
			Dst:         "100.0.0.0/24",
			Want:        nil,
		},
		{
			Name:        "priority_out_of_range",
			Description: "priority must be within 100-200 range",
			ExpectError: true,
			Priority:    400,
			Table:       100,
			Src:         "100.0.0.0/24",
			Dst:         "100.0.0.0/24",
			Want:        nil,
		},
		{
			Name:        "invalid_src_net",
			Description: "src network is invalid",
			ExpectError: true,
			Priority:    100,
			Table:       100,
			Src:         "100.0.0.0.0/24",
			Dst:         "100.0.0.0/24",
			Want:        nil,
		},
		{
			Name:        "invalid_dst_net",
			Description: "src network is invalid",
			ExpectError: true,
			Priority:    100,
			Table:       100,
			Src:         "100.0.0.0/24",
			Dst:         "100.0.0.0.0/24",
			Want:        nil,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			got, err := routing.NewIPRule(test.Priority, test.Table, test.Src, test.Dst)
			if err != nil && !test.ExpectError {
				t.Errorf("error: %v", err)
			}
			if err == nil && test.ExpectError {
				t.Errorf("wanted error but returned nil")
			} else {
				if diff := cmp.Diff(test.Want, got); diff != "" {
					t.Errorf("IP rule mismatch (-want +got): %s\n", diff)
				}
			}
		})
	}
}
