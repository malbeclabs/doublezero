package routing

import (
	"fmt"
	"net"

	nl "github.com/vishvananda/netlink"
)

const (
	RouteTableSpecific = 100
	RouteTableDefault  = 101
)

type Route struct {
	Dst      *net.IPNet
	Src      net.IP
	Table    int
	NextHop  net.IP
	Protocol int
}

func NewRoute(table int, dst *net.IPNet, src, nexthop net.IP, protocol int) *Route {
	return &Route{
		Table:    table,
		Dst:      dst,
		Src:      src,
		NextHop:  nexthop,
		Protocol: protocol,
	}
}

func (r *Route) String() string {
	return fmt.Sprintf(
		"table: %d, dst: %s, src: %s, nh: %s protocol: %s", r.Table, r.Dst, r.Src, r.NextHop, nl.RouteProtocol(r.Protocol))
}
