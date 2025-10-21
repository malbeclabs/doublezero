package manager

import "github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"

// NetlinkerRouteManager is a wrapper around the netlinker that implements the RouteManager interface.
type NetlinkerRouteManager struct {
	nlr routing.Netlinker
}

func NewNetlinkerRouteManager(nlr routing.Netlinker) *NetlinkerRouteManager {
	return &NetlinkerRouteManager{nlr: nlr}
}

func (n *NetlinkerRouteManager) PeerOnEstablished() error {
	return nil
}

func (n *NetlinkerRouteManager) PeerOnClose() error {
	return nil
}

func (n *NetlinkerRouteManager) RouteAdd(route *routing.Route) error {
	return n.nlr.RouteAdd(route)
}

func (n *NetlinkerRouteManager) RouteDelete(route *routing.Route) error {
	return n.nlr.RouteDelete(route)
}

func (n *NetlinkerRouteManager) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	return n.nlr.RouteByProtocol(protocol)
}
