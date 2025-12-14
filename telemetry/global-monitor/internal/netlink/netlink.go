package netlink

import "net"

type Route struct {
	Dst *net.IPNet
	Src net.IP
	Gw  net.IP
}

type Netlinker interface {
	GetBGPRoutesByDst() (map[string]Route, error)
}
