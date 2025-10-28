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
	if r == nil {
		return ""
	}
	var dst, src, nexthop string
	if r.Dst != nil {
		dst = r.Dst.String()
	}
	if r.Src != nil {
		src = r.Src.String()
	}
	if r.NextHop != nil {
		nexthop = r.NextHop.String()
	}
	return fmt.Sprintf("table: %d, dst: %s, src: %s, nh: %s protocol: %s", r.Table, dst, src, nexthop, nl.RouteProtocol(r.Protocol))
}
