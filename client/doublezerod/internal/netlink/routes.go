package netlink

import (
	"fmt"
	"net"
)

const (
	RouteTableSpecific = 100
	RouteTableDefault  = 101
)

type Route struct {
	Dst     *net.IPNet
	Src     net.IP
	Table   int
	NextHop net.IP
}

func NewRoute(table int, dst *net.IPNet, src, nexthop net.IP) *Route {
	return &Route{
		Table:   table,
		Dst:     dst,
		Src:     src,
		NextHop: nexthop,
	}
}

func (r *Route) String() string {
	return fmt.Sprintf(
		"table: %d, dst: %s, src: %s, nh: %s", r.Table, r.Dst, r.Src, r.NextHop)
}
