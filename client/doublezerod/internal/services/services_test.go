package services_test

import (
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jwhited/corebgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/manager"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"golang.org/x/sys/unix"
)

type MockBgpServer struct {
	deletedPeer net.IP
	addPeer     *bgp.PeerConfig
}

func (m *MockBgpServer) Serve(lis []net.Listener) error { return nil }
func (m *MockBgpServer) AddPeer(p *bgp.PeerConfig, nlri []bgp.NLRI) error {
	m.addPeer = p
	return nil
}

func (m *MockBgpServer) DeletePeer(ip net.IP) error {
	m.deletedPeer = ip
	return nil
}
func (m *MockBgpServer) GetPeerStatus(net.IP) bgp.Session { return bgp.Session{} }
func (m *MockBgpServer) Close()                           {}
func (m *MockBgpServer) GetPeers() []corebgp.PeerConfig   { return []corebgp.PeerConfig{} }

type MockNetlink struct {
	routes        []*routing.Route
	routesAdded   []*routing.Route
	routesRemoved []*routing.Route
	tunAdded      *routing.Tunnel
	tunRemoved    *routing.Tunnel
	tunAddrAdded  []MockTunAddr
	tunUp         bool
	ruleAdded     []*routing.IPRule
	ruleRemoved   []*routing.IPRule
	callLog       []string
}

type MockTunAddr struct {
	IP string
}

func (m *MockNetlink) TunnelAdd(t *routing.Tunnel) error {
	m.tunAdded = t
	return nil
}

func (m *MockNetlink) TunnelDelete(n *routing.Tunnel) error {
	m.callLog = append(m.callLog, "TunnelDelete")
	m.tunRemoved = n
	return nil
}

func (m *MockNetlink) TunnelAddrAdd(t *routing.Tunnel, ip string) error {
	m.tunAddrAdded = append(m.tunAddrAdded, MockTunAddr{IP: ip})
	return nil
}

func (m *MockNetlink) TunnelUp(t *routing.Tunnel) error {
	m.tunUp = true
	return nil
}

func (m *MockNetlink) RouteAdd(r *routing.Route) error {
	m.routesAdded = append(m.routesAdded, r)
	return nil
}

func (m *MockNetlink) RouteDelete(n *routing.Route) error {
	m.callLog = append(m.callLog, "RouteDelete")
	m.routesRemoved = append(m.routesRemoved, n)
	return nil
}

func (m *MockNetlink) RouteGet(net.IP) ([]*routing.Route, error) {
	return m.routes, nil
}

func (m *MockNetlink) RuleAdd(r *routing.IPRule) error {
	m.ruleAdded = append(m.ruleAdded, r)
	return nil
}

func (m *MockNetlink) RuleDel(n *routing.IPRule) error {
	m.callLog = append(m.callLog, "RuleDel")
	m.ruleRemoved = append(m.ruleRemoved, n)
	return nil
}

func (m *MockNetlink) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	return m.routes, nil
}

type MockDb struct {
	state []*api.ProvisionRequest
}

func (m *MockDb) GetState(usertypes ...api.UserType) []*api.ProvisionRequest {
	return m.state
}

func (m *MockDb) DeleteState(u api.UserType) error        { return nil }
func (m *MockDb) SaveState(p *api.ProvisionRequest) error { return nil }

type MockPIMServer struct{}

func (m *MockPIMServer) Start(conn pim.RawConner, iface string, tunnelAddr net.IP, group []net.IP) error {
	return nil
}

func (m *MockPIMServer) Close() error {
	return nil
}

type MockHeartbeatSender struct {
	started bool
	closed  bool
	iface   string
	srcIP   net.IP
	groups  []net.IP
	ttl     int
}

func (m *MockHeartbeatSender) Start(iface string, srcIP net.IP, groups []net.IP, ttl int, interval time.Duration) error {
	m.started = true
	m.iface = iface
	m.srcIP = srcIP
	m.groups = groups
	m.ttl = ttl
	return nil
}

func (m *MockHeartbeatSender) Close() error {
	m.closed = true
	return nil
}

func TestServices(t *testing.T) {
	tests := []struct {
		name                string
		provisioningRequest *api.ProvisionRequest
		userType            api.UserType
		expectError         bool
		wantRulesAdded      []*routing.IPRule
		wantRulesRemoved    []*routing.IPRule
		wantRoutesAdded     []*routing.Route
		wantRoutesRemoved   []*routing.Route
		wantTunAddrAdded    []MockTunAddr
		wantTunAdded        *routing.Tunnel
		wantTunUp           bool
		wantTunRemoved      *routing.Tunnel
		wantPeerConfig      *bgp.PeerConfig
		wantPeerRemoved     net.IP
	}{
		{
			name: "provision_ibrl",
			provisioningRequest: &api.ProvisionRequest{
				UserType:  api.UserTypeIBRL,
				TunnelSrc: net.IPv4(192, 168, 1, 1),
				TunnelDst: net.IPv4(192, 168, 1, 2),
				TunnelNet: &net.IPNet{
					IP:   net.IPv4(169, 254, 0, 0),
					Mask: net.CIDRMask(31, 32),
				},
				DoubleZeroIP:       net.IPv4(192, 168, 1, 1),
				DoubleZeroPrefixes: []*net.IPNet{},
				BgpLocalAsn:        65000,
				BgpRemoteAsn:       65001,
			},
			userType:    api.UserTypeIBRL,
			expectError: false,
			wantTunAdded: &routing.Tunnel{
				Name:           "doublezero0",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(192, 168, 1, 1),
				RemoteUnderlay: net.IPv4(192, 168, 1, 2),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantTunAddrAdded: []MockTunAddr{{IP: "169.254.0.1/31"}},
			wantTunUp:        true,
			wantRulesAdded:   nil,
			wantRoutesAdded:  nil,
			wantPeerConfig: &bgp.PeerConfig{
				LocalAddress:         net.IPv4(169, 254, 0, 1),
				RemoteAddress:        net.IPv4(169, 254, 0, 0),
				LocalAs:              65000,
				RemoteAs:             65001,
				RouteSrc:             net.IPv4(192, 168, 1, 1),
				RouteTable:           syscall.RT_TABLE_MAIN,
				NoUninstall:          false,
				Interface:            "doublezero0",
				AllowLivenessEnabled: true,
			},
			wantTunRemoved: &routing.Tunnel{
				Name:           "doublezero0",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(192, 168, 1, 1),
				RemoteUnderlay: net.IPv4(192, 168, 1, 2),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantPeerRemoved: net.IP{169, 254, 0, 0},
		},
		{
			name: "provision_ibrl_with_allocated_ip",
			provisioningRequest: &api.ProvisionRequest{
				UserType:  api.UserTypeIBRLWithAllocatedIP,
				TunnelSrc: net.IPv4(192, 168, 1, 0),
				TunnelDst: net.IPv4(192, 168, 1, 1),
				TunnelNet: &net.IPNet{
					IP:   net.IPv4(169, 254, 0, 0),
					Mask: net.CIDRMask(31, 32),
				},
				DoubleZeroIP:       net.IPv4(192, 168, 1, 0),
				DoubleZeroPrefixes: []*net.IPNet{},
				BgpLocalAsn:        65000,
				BgpRemoteAsn:       65001,
			},
			userType:    api.UserTypeIBRLWithAllocatedIP,
			expectError: false,
			wantTunAdded: &routing.Tunnel{
				Name:           "doublezero0",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(192, 168, 1, 0),
				RemoteUnderlay: net.IPv4(192, 168, 1, 1),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantTunAddrAdded: []MockTunAddr{{IP: "169.254.0.1/31"}, {IP: "192.168.1.0/32"}},
			wantTunUp:        true,
			wantRulesAdded:   nil,
			wantRoutesAdded:  nil,
			wantPeerConfig: &bgp.PeerConfig{
				LocalAddress:         net.IPv4(169, 254, 0, 1),
				RemoteAddress:        net.IPv4(169, 254, 0, 0),
				LocalAs:              65000,
				RemoteAs:             65001,
				RouteSrc:             net.IPv4(192, 168, 1, 0),
				RouteTable:           syscall.RT_TABLE_MAIN,
				NoUninstall:          true,
				Interface:            "doublezero0",
				AllowLivenessEnabled: true,
			},
			wantTunRemoved: &routing.Tunnel{
				Name:           "doublezero0",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(192, 168, 1, 0),
				RemoteUnderlay: net.IPv4(192, 168, 1, 1),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantPeerRemoved: net.IP{169, 254, 0, 0},
		},
		{
			name: "provision_edge_filtering",
			provisioningRequest: &api.ProvisionRequest{
				UserType:  api.UserTypeEdgeFiltering,
				TunnelSrc: net.IPv4(1, 1, 1, 1),
				TunnelDst: net.IPv4(2, 2, 2, 2),
				TunnelNet: &net.IPNet{
					IP:   net.IPv4(169, 254, 0, 0),
					Mask: net.IPMask{255, 255, 255, 254},
				},
				DoubleZeroIP: net.IPv4(7, 7, 7, 7),
				DoubleZeroPrefixes: []*net.IPNet{
					{IP: net.IP{7, 0, 0, 0}, Mask: net.IPMask{255, 0, 0, 0}},
				},
				BgpLocalAsn:  65000,
				BgpRemoteAsn: 65001,
			},
			userType:    api.UserTypeEdgeFiltering,
			expectError: false,
			wantTunAdded: &routing.Tunnel{
				Name:           "doublezero0",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantTunAddrAdded: []MockTunAddr{{IP: "169.254.0.1/31"}, {IP: "7.7.7.7/32"}},
			wantTunUp:        true,
			wantRulesAdded: []*routing.IPRule{
				{
					Priority: 100,
					Table:    routing.DzTableSpecific,
					SrcNet:   &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
					DstNet:   &net.IPNet{IP: net.IPv4(7, 0, 0, 0), Mask: []byte{255, 0, 0, 0}},
				},
				{
					Priority: 101,
					Table:    101,
					SrcNet:   &net.IPNet{IP: net.IPv4(7, 0, 0, 0), Mask: []byte{255, 0, 0, 0}},
					DstNet:   &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
				},
			},
			wantRoutesAdded: []*routing.Route{
				{
					Table:    101,
					Dst:      &net.IPNet{IP: net.IP{0, 0, 0, 0}, Mask: net.IPMask{0, 0, 0, 0}},
					Src:      net.IP{7, 7, 7, 7},
					NextHop:  net.IP{169, 254, 0, 0},
					Protocol: unix.RT_CLASS_UNSPEC,
				},
			},
			wantPeerConfig: &bgp.PeerConfig{
				LocalAddress:  net.IPv4(169, 254, 0, 1),
				RemoteAddress: net.IPv4(169, 254, 0, 0),
				LocalAs:       65000,
				RemoteAs:      65001,
				RouteSrc:      net.IPv4(7, 7, 7, 7),
				RouteTable:    100, // ?
				NoUninstall:   false,
			},
			wantTunRemoved: &routing.Tunnel{
				Name:           "doublezero0",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantPeerRemoved: net.IP{169, 254, 0, 0},
			wantRulesRemoved: []*routing.IPRule{
				{
					Priority: 100,
					Table:    routing.DzTableSpecific,
					SrcNet:   &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
					DstNet:   &net.IPNet{IP: net.IPv4(7, 0, 0, 0), Mask: []byte{255, 0, 0, 0}},
				},
				{
					Priority: 101,
					Table:    101,
					SrcNet:   &net.IPNet{IP: net.IPv4(7, 0, 0, 0), Mask: []byte{255, 0, 0, 0}},
					DstNet:   &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
				},
			},
			wantRoutesRemoved: []*routing.Route{
				{
					Table:    101,
					Dst:      &net.IPNet{IP: net.IP{0, 0, 0, 0}, Mask: net.IPMask{0, 0, 0, 0}},
					Src:      net.IP{7, 7, 7, 7},
					NextHop:  net.IP{169, 254, 0, 0},
					Protocol: unix.RT_CLASS_UNSPEC,
				},
			},
		},
		{
			name: "provision_multicast_subscriber",
			provisioningRequest: &api.ProvisionRequest{
				UserType:           api.UserTypeMulticast,
				TunnelSrc:          net.IPv4(1, 1, 1, 1),
				TunnelDst:          net.IPv4(2, 2, 2, 2),
				MulticastSubGroups: []net.IP{{239, 0, 0, 1}},
				TunnelNet: &net.IPNet{
					IP:   net.IPv4(169, 254, 0, 0),
					Mask: net.IPMask{255, 255, 255, 254},
				},
				DoubleZeroIP:       net.IPv4(7, 7, 7, 7),
				DoubleZeroPrefixes: []*net.IPNet{},
				BgpLocalAsn:        65000,
				BgpRemoteAsn:       65001,
			},
			userType:    api.UserTypeMulticast,
			expectError: false,
			wantTunAdded: &routing.Tunnel{
				Name:           "doublezero1",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantTunAddrAdded: []MockTunAddr{{IP: "169.254.0.1/31"}},
			wantTunUp:        true,
			wantRulesAdded:   nil,
			wantRoutesAdded: []*routing.Route{
				{
					Table:    syscall.RT_TABLE_MAIN,
					Dst:      &net.IPNet{IP: net.IP{239, 0, 0, 1}, Mask: net.IPMask{255, 255, 255, 255}},
					NextHop:  net.IP{169, 254, 0, 0},
					Src:      nil,
					Protocol: unix.RTPROT_STATIC,
				}},
			wantPeerConfig: &bgp.PeerConfig{
				LocalAddress:  net.IPv4(169, 254, 0, 1),
				RemoteAddress: net.IPv4(169, 254, 0, 0),
				LocalAs:       65000,
				RemoteAs:      65001,
				NoInstall:     true,
			},
			wantTunRemoved: &routing.Tunnel{
				Name:           "doublezero1",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantPeerRemoved: net.IP{169, 254, 0, 0},
		},
		{
			name: "provision_multicast_publisher_and_subscriber",
			provisioningRequest: &api.ProvisionRequest{
				UserType:           api.UserTypeMulticast,
				TunnelSrc:          net.IPv4(1, 1, 1, 1),
				TunnelDst:          net.IPv4(2, 2, 2, 2),
				MulticastPubGroups: []net.IP{{239, 0, 0, 1}},
				MulticastSubGroups: []net.IP{{239, 0, 0, 2}},
				TunnelNet: &net.IPNet{
					IP:   net.IPv4(169, 254, 0, 0),
					Mask: net.IPMask{255, 255, 255, 254},
				},
				DoubleZeroIP:       net.IPv4(7, 7, 7, 7),
				DoubleZeroPrefixes: []*net.IPNet{},
				BgpLocalAsn:        65000,
				BgpRemoteAsn:       65001,
			},
			userType:    api.UserTypeMulticast,
			expectError: false,
			wantTunAdded: &routing.Tunnel{
				Name:           "doublezero1",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantTunAddrAdded: []MockTunAddr{{IP: "169.254.0.1/31"}, {IP: "7.7.7.7/32"}},
			wantTunUp:        true,
			wantRulesAdded:   nil,
			wantRoutesAdded: []*routing.Route{
				{
					Table:    syscall.RT_TABLE_MAIN,
					Dst:      &net.IPNet{IP: net.IP{239, 0, 0, 1}, Mask: net.IPMask{255, 255, 255, 255}},
					NextHop:  net.IP{169, 254, 0, 0},
					Src:      net.IP{7, 7, 7, 7},
					Protocol: unix.RTPROT_STATIC,
				},
				{
					Table:    syscall.RT_TABLE_MAIN,
					Dst:      &net.IPNet{IP: net.IP{239, 0, 0, 2}, Mask: net.IPMask{255, 255, 255, 255}},
					NextHop:  net.IP{169, 254, 0, 0},
					Src:      nil,
					Protocol: unix.RTPROT_STATIC,
				},
			},
			wantPeerConfig: &bgp.PeerConfig{
				LocalAddress:  net.IPv4(169, 254, 0, 1),
				RemoteAddress: net.IPv4(169, 254, 0, 0),
				LocalAs:       65000,
				RemoteAs:      65001,
				NoInstall:     true,
			},
			wantTunRemoved: &routing.Tunnel{
				Name:           "doublezero1",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantPeerRemoved: net.IP{169, 254, 0, 0},
		},
		{
			name: "provision_multicast_publisher_and_subscriber_same_group",
			provisioningRequest: &api.ProvisionRequest{
				UserType:           api.UserTypeMulticast,
				TunnelSrc:          net.IPv4(1, 1, 1, 1),
				TunnelDst:          net.IPv4(2, 2, 2, 2),
				MulticastPubGroups: []net.IP{{239, 0, 0, 1}},
				MulticastSubGroups: []net.IP{{239, 0, 0, 1}},
				TunnelNet: &net.IPNet{
					IP:   net.IPv4(169, 254, 0, 0),
					Mask: net.IPMask{255, 255, 255, 254},
				},
				DoubleZeroIP:       net.IPv4(7, 7, 7, 7),
				DoubleZeroPrefixes: []*net.IPNet{},
				BgpLocalAsn:        65000,
				BgpRemoteAsn:       65001,
			},
			userType:    api.UserTypeMulticast,
			expectError: false,
			wantTunAdded: &routing.Tunnel{
				Name:           "doublezero1",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantTunAddrAdded: []MockTunAddr{{IP: "169.254.0.1/31"}, {IP: "7.7.7.7/32"}},
			wantTunUp:        true,
			wantRulesAdded:   nil,
			// Only one route — the publisher route with Src set. The subscriber
			// skips the duplicate group because it's already routed by the publisher.
			wantRoutesAdded: []*routing.Route{
				{
					Table:    syscall.RT_TABLE_MAIN,
					Dst:      &net.IPNet{IP: net.IP{239, 0, 0, 1}, Mask: net.IPMask{255, 255, 255, 255}},
					NextHop:  net.IP{169, 254, 0, 0},
					Src:      net.IP{7, 7, 7, 7},
					Protocol: unix.RTPROT_STATIC,
				},
			},
			wantPeerConfig: &bgp.PeerConfig{
				LocalAddress:  net.IPv4(169, 254, 0, 1),
				RemoteAddress: net.IPv4(169, 254, 0, 0),
				LocalAs:       65000,
				RemoteAs:      65001,
				NoInstall:     true,
			},
			wantTunRemoved: &routing.Tunnel{
				Name:           "doublezero1",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantPeerRemoved: net.IP{169, 254, 0, 0},
		},
		{
			name: "provision_multicast_publisher",
			provisioningRequest: &api.ProvisionRequest{
				UserType:           api.UserTypeMulticast,
				TunnelSrc:          net.IPv4(1, 1, 1, 1),
				TunnelDst:          net.IPv4(2, 2, 2, 2),
				MulticastPubGroups: []net.IP{{239, 0, 0, 1}},
				TunnelNet: &net.IPNet{
					IP:   net.IPv4(169, 254, 0, 0),
					Mask: net.IPMask{255, 255, 255, 254},
				},
				DoubleZeroIP:       net.IPv4(7, 7, 7, 7),
				DoubleZeroPrefixes: []*net.IPNet{},
				BgpLocalAsn:        65000,
				BgpRemoteAsn:       65001,
			},
			userType:    api.UserTypeMulticast,
			expectError: false,
			wantTunAdded: &routing.Tunnel{
				Name:           "doublezero1",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantTunAddrAdded: []MockTunAddr{{IP: "169.254.0.1/31"}, {IP: "7.7.7.7/32"}},
			wantTunUp:        true,
			wantRulesAdded:   nil,
			wantRoutesAdded: []*routing.Route{
				{
					Table:    syscall.RT_TABLE_MAIN,
					Dst:      &net.IPNet{IP: net.IP{239, 0, 0, 1}, Mask: net.IPMask{255, 255, 255, 255}},
					NextHop:  net.IP{169, 254, 0, 0},
					Src:      net.IP{7, 7, 7, 7},
					Protocol: unix.RTPROT_STATIC,
				},
			},
			wantPeerConfig: &bgp.PeerConfig{
				LocalAddress:  net.IPv4(169, 254, 0, 1),
				RemoteAddress: net.IPv4(169, 254, 0, 0),
				LocalAs:       65000,
				RemoteAs:      65001,
				NoInstall:     true,
			},
			wantTunRemoved: &routing.Tunnel{
				Name:           "doublezero1",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(169, 254, 0, 1),
				RemoteOverlay:  net.IPv4(169, 254, 0, 0),
				MTU:            routing.GREMTU,
			},
			wantPeerRemoved: net.IP{169, 254, 0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockBgp := &MockBgpServer{}
			mockNetlink := &MockNetlink{}
			mockDb := &MockDb{}
			mockPim := &MockPIMServer{}
			mockHeartbeat := &MockHeartbeatSender{}

			svc, err := manager.CreateService(tt.userType, mockBgp, mockNetlink, mockDb, mockPim, mockHeartbeat)
			if err != nil {
				t.Fatalf("failed to create service: %v", err)
			}
			if err = svc.Setup(tt.provisioningRequest); err != nil {
				if !tt.expectError {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if tt.expectError {
				t.Fatal("expected error but got none")
			}

			t.Run("check_tunnel_added", func(t *testing.T) {
				if diff := cmp.Diff(mockNetlink.tunAdded, tt.wantTunAdded); diff != "" {
					t.Errorf("unexpected tunnel added (-want +got):\n%s", diff)
				}
			})

			t.Run("check_tunnel_addresses_added", func(t *testing.T) {
				if diff := cmp.Diff(mockNetlink.tunAddrAdded, tt.wantTunAddrAdded); diff != "" {
					t.Errorf("unexpected tunnel address added (-want +got):\n%s", diff)
				}
			})
			t.Run("check_tunnel_up", func(t *testing.T) {
				if mockNetlink.tunUp != tt.wantTunUp {
					t.Errorf("unexpected tunnel up status: got %t, want %t", mockNetlink.tunUp, tt.wantTunUp)
				}
			})

			t.Run("check_rules_added", func(t *testing.T) {
				if diff := cmp.Diff(mockNetlink.ruleAdded, tt.wantRulesAdded); diff != "" {
					t.Errorf("unexpected rules added (-want +got):\n%s", diff)
				}
			})
			t.Run("check_routes_added", func(t *testing.T) {
				if diff := cmp.Diff(tt.wantRoutesAdded, mockNetlink.routesAdded); diff != "" {
					t.Errorf("unexpected routes added (-want +got):\n%s", diff)
				}
			})

			t.Run("check_peer_added", func(t *testing.T) {
				if diff := cmp.Diff(mockBgp.addPeer, tt.wantPeerConfig); diff != "" {
					t.Errorf("unexpected peer added (-want +got):\n%s", diff)
				}
			})

			if err = svc.Teardown(); err != nil {
				t.Fatalf("unexpected error during teardown: %v", err)
			}

			t.Run("check_peer_delete", func(t *testing.T) {
				if diff := cmp.Diff(mockBgp.deletedPeer, tt.wantPeerRemoved); diff != "" {
					t.Errorf("unexpected peer removed (-want +got):\n%s", diff)
				}
			})

			t.Run("check_tunnel_removed", func(t *testing.T) {
				if diff := cmp.Diff(mockNetlink.tunRemoved, tt.wantTunRemoved); diff != "" {
					t.Errorf("unexpected tunnel removed (-want +got):\n%s", diff)
				}
			})
			t.Run("check_routes_removed", func(t *testing.T) {
				if diff := cmp.Diff(mockNetlink.routesRemoved, tt.wantRoutesRemoved); diff != "" {
					t.Errorf("unexpected routes removed (-want +got):\n%s", diff)
				}
			})
			t.Run("check_rules_removed", func(t *testing.T) {
				if diff := cmp.Diff(mockNetlink.ruleRemoved, tt.wantRulesRemoved); diff != "" {
					t.Errorf("unexpected rules removed (-want +got):\n%s", diff)
				}
			})
		})
	}
}

func TestMulticastService_DoubleTeardown(t *testing.T) {
	mockBgp := &MockBgpServer{}
	mockNetlink := &MockNetlink{}
	mockDb := &MockDb{}
	mockPim := &MockPIMServer{}
	mockHeartbeat := &MockHeartbeatSender{}

	svc, err := manager.CreateService(api.UserTypeMulticast, mockBgp, mockNetlink, mockDb, mockPim, mockHeartbeat)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	pr := &api.ProvisionRequest{
		UserType:           api.UserTypeMulticast,
		TunnelSrc:          net.IPv4(1, 1, 1, 1),
		TunnelDst:          net.IPv4(2, 2, 2, 2),
		MulticastPubGroups: []net.IP{{239, 0, 0, 1}},
		TunnelNet: &net.IPNet{
			IP:   net.IPv4(169, 254, 0, 0),
			Mask: net.IPMask{255, 255, 255, 254},
		},
		DoubleZeroIP:       net.IPv4(7, 7, 7, 7),
		DoubleZeroPrefixes: []*net.IPNet{},
		BgpLocalAsn:        65000,
		BgpRemoteAsn:       65001,
	}

	if err := svc.Setup(pr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First teardown should succeed.
	if err := svc.Teardown(); err != nil {
		t.Fatalf("first Teardown() returned error: %v", err)
	}

	// Second teardown must not panic (e.g. double close of heartbeat channel).
	if err := svc.Teardown(); err != nil {
		t.Fatalf("second Teardown() returned error: %v", err)
	}
}
