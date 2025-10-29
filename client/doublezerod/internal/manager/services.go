//go:build linux

package manager

import (
	"fmt"
	"net"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/services"
)

func CreatePassthroughService(userType api.UserType, bgps BGPServer, nlr routing.Netlinker, db services.DBReaderWriter, pim services.PIMWriter) (Provisioner, error) {
	switch userType {
	case api.UserTypeIBRL:
		return services.NewIBRLService(bgps, nlr, db, func(iface string, src net.IP) (bgp.RouteManager, error) {
			return NewNetlinkerPassthroughRouteManager(nlr), nil
		}), nil
	case api.UserTypeIBRLWithAllocatedIP:
		return services.NewIBRLServiceWithAllocatedAddress(bgps, nlr, db, func(iface string, src net.IP) (bgp.RouteManager, error) {
			return NewNetlinkerPassthroughRouteManager(nlr), nil
		}), nil
	case api.UserTypeEdgeFiltering:
		return services.NewEdgeFilteringService(bgps, nlr, db, func(iface string, src net.IP) (bgp.RouteManager, error) {
			return NewNetlinkerPassthroughRouteManager(nlr), nil
		}), nil
	case api.UserTypeMulticast:
		return services.NewMulticastService(bgps, nlr, db, pim, func(iface string, src net.IP) (bgp.RouteManager, error) {
			return NewNetlinkerPassthroughRouteManager(nlr), nil
		}), nil
	default:
		return nil, fmt.Errorf("unsupported user type: %s", userType)
	}
}

// NetlinkerPassthroughRouteManager is route manager implementation that passes through all calls to the netlinker.
type NetlinkerPassthroughRouteManager struct {
	nlr routing.Netlinker
}

func NewNetlinkerPassthroughRouteManager(nlr routing.Netlinker) *NetlinkerPassthroughRouteManager {
	return &NetlinkerPassthroughRouteManager{nlr: nlr}
}

func (n *NetlinkerPassthroughRouteManager) PeerOnEstablished() error {
	return nil
}

func (n *NetlinkerPassthroughRouteManager) PeerOnClose() error {
	return nil
}

func (n *NetlinkerPassthroughRouteManager) RouteAdd(route *routing.Route) error {
	return n.nlr.RouteAdd(route)
}

func (n *NetlinkerPassthroughRouteManager) RouteDelete(route *routing.Route) error {
	return n.nlr.RouteDelete(route)
}

func (n *NetlinkerPassthroughRouteManager) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	return n.nlr.RouteByProtocol(protocol)
}
