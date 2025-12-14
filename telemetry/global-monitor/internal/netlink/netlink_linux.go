package netlink

import (
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func NewNetlinker() Netlinker {
	return &linuxNetlink{}
}

type linuxNetlink struct{}

func (d *linuxNetlink) GetBGPRoutesByDst() (map[string]Route, error) {
	routeFilter := &netlink.Route{
		Protocol: netlink.RouteProtocol(unix.RTPROT_BGP),
	}

	routesFromNLR, err := netlink.RouteListFiltered(netlink.FAMILY_V4, routeFilter, netlink.RT_FILTER_PROTOCOL)
	if err != nil {
		return nil, err
	}

	routes := make(map[string]Route)
	for _, r := range routesFromNLR {
		route := Route{
			Src: r.Src,
			Gw:  r.Gw,
			Dst: r.Dst,
		}
		if route.Dst == nil || route.Dst.IP == nil || route.Dst.IP.To4() == nil {
			continue
		}
		routes[route.Dst.IP.To4().String()] = route
	}
	return routes, nil
}
