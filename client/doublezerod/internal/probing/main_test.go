package probing

import (
	"net"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type MockNetlinker struct {
	TunnelAddFunc       func(*routing.Tunnel) error
	TunnelDeleteFunc    func(*routing.Tunnel) error
	TunnelAddrAddFunc   func(*routing.Tunnel, string) error
	TunnelUpFunc        func(*routing.Tunnel) error
	RouteAddFunc        func(*routing.Route) error
	RouteDeleteFunc     func(*routing.Route) error
	RouteGetFunc        func(net.IP) ([]*routing.Route, error)
	RuleAddFunc         func(*routing.IPRule) error
	RuleDelFunc         func(*routing.IPRule) error
	RouteByProtocolFunc func(int) ([]*routing.Route, error)
}

func (m *MockNetlinker) TunnelAdd(t *routing.Tunnel) error {
	return m.TunnelAddFunc(t)
}

func (m *MockNetlinker) TunnelDelete(t *routing.Tunnel) error {
	return m.TunnelDeleteFunc(t)
}

func (m *MockNetlinker) TunnelAddrAdd(t *routing.Tunnel, ip string) error {
	return m.TunnelAddrAddFunc(t, ip)
}

func (m *MockNetlinker) TunnelUp(t *routing.Tunnel) error {
	return m.TunnelUpFunc(t)
}

func (m *MockNetlinker) RouteAdd(r *routing.Route) error {
	return m.RouteAddFunc(r)
}

func (m *MockNetlinker) RouteDelete(r *routing.Route) error {
	return m.RouteDeleteFunc(r)
}

func (m *MockNetlinker) RouteGet(ip net.IP) ([]*routing.Route, error) {
	return m.RouteGetFunc(ip)
}

func (m *MockNetlinker) RuleAdd(r *routing.IPRule) error {
	return m.RuleAddFunc(r)
}

func (m *MockNetlinker) RuleDel(r *routing.IPRule) error {
	return m.RuleDelFunc(r)
}

func (m *MockNetlinker) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	return m.RouteByProtocolFunc(protocol)
}
