package routing

import (
	"net"
	"testing"
)

func TestRouteToNetlinkSetsGREMTUAndLock(t *testing.T) {
	_, dst, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatalf("failed to parse dst: %v", err)
	}

	route := &Route{
		Dst:      dst,
		Src:      net.IPv4(192, 0, 2, 10),
		Table:    RouteTableSpecific,
		NextHop:  net.IPv4(192, 0, 2, 1),
		Protocol: 186,
	}

	got := routeToNetlink(route)
	if got.MTU != GREMTU {
		t.Fatalf("route MTU should be GRE MTU %d; got %d", GREMTU, got.MTU)
	}
	if !got.MTULock {
		t.Fatal("route MTU lock should be enabled")
	}
}
