package rpc

import (
	"net"

	"github.com/vishvananda/netlink"
)

type Route struct {
	Dst *net.IPNet
	Src net.IP
	Gw  net.IP
}

type Netlink struct{}

func (d *Netlink) RouteGet(dest net.IP) ([]Route, error) {
	routes, err := netlink.RouteGet(dest)
	if err != nil {
		return nil, err
	}
	var result []Route
	for _, r := range routes {
		route := Route{
			Src: r.Src,
			Gw:  r.Gw,
			Dst: r.Dst,
		}
		result = append(result, route)
	}
	return result, nil
}
