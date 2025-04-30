package netlink_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/netlink"
)

type MockBgpServer struct {
	deletedPeer net.IP
}

func (m *MockBgpServer) Serve(lis []net.Listener) error                   { return nil }
func (m *MockBgpServer) AddPeer(p *bgp.PeerConfig, nlri []bgp.NLRI) error { return nil }
func (m *MockBgpServer) DeletePeer(ip net.IP) error {
	m.deletedPeer = ip
	return nil
}
func (m *MockBgpServer) AddRoute() <-chan bgp.NLRI        { return nil }
func (m *MockBgpServer) WithdrawRoute() <-chan bgp.NLRI   { return nil }
func (m *MockBgpServer) FlushRoutes() <-chan struct{}     { return nil }
func (m *MockBgpServer) GetPeerStatus(net.IP) bgp.Session { return bgp.Session{} }

type MockNetlink struct {
	routes        []*netlink.Route
	routesAdded   []*netlink.Route
	routesRemoved []*netlink.Route
	tunAdded      *netlink.Tunnel
	tunRemoved    *netlink.Tunnel
	tunAddrAdded  []string
	tunUp         *netlink.Tunnel
	ruleAdded     []*netlink.IPRule
	ruleRemoved   []*netlink.IPRule
	callLog       []string
}

func (m *MockNetlink) TunnelAdd(t *netlink.Tunnel) error {
	m.tunAdded = t
	return nil
}
func (m *MockNetlink) TunnelDelete(n *netlink.Tunnel) error {
	m.callLog = append(m.callLog, "TunnelDelete")
	m.tunRemoved = n
	return nil
}
func (m *MockNetlink) TunnelAddrAdd(t *netlink.Tunnel, ip string) error {
	m.tunAddrAdded = append(m.tunAddrAdded, ip)
	return nil
}
func (m *MockNetlink) TunnelUp(t *netlink.Tunnel) error {
	m.tunUp = t
	return nil
}
func (m *MockNetlink) RouteAdd(r *netlink.Route) error {
	m.routesAdded = append(m.routesAdded, r)
	return nil
}
func (m *MockNetlink) RouteDelete(n *netlink.Route) error {
	m.callLog = append(m.callLog, "RouteDelete")
	m.routesRemoved = append(m.routesRemoved, n)
	return nil
}
func (m *MockNetlink) RouteGet(net.IP) ([]*netlink.Route, error) {
	return m.routes, nil
}
func (m *MockNetlink) RuleAdd(r *netlink.IPRule) error {
	m.ruleAdded = append(m.ruleAdded, r)
	return nil
}
func (m *MockNetlink) RuleDel(n *netlink.IPRule) error {
	m.callLog = append(m.callLog, "RuleDel")
	m.ruleRemoved = append(m.ruleRemoved, n)
	return nil
}

type MockDb struct {
	state *netlink.ProvisionRequest
}

func (m *MockDb) GetState() *netlink.ProvisionRequest {
	return m.state
}

func (m *MockDb) DeleteState() error                          { return nil }
func (m *MockDb) SaveState(p *netlink.ProvisionRequest) error { return nil }

func TestNetlinkManager_DiscoverTunnelSource(t *testing.T) {

	tests := []struct {
		Name        string
		Description string
		ExpectError bool
		TunnelDst   net.IP
		Routes      []*netlink.Route
		Want        net.IP
	}{
		{
			Name:        "single_route_found_happy_path",
			Description: "single default route returned and src address is found",
			ExpectError: false,
			TunnelDst:   net.IPv4(1, 1, 1, 1),
			Routes: []*netlink.Route{{
				Dst:     &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
				Src:     net.IPv4(2, 2, 2, 2),
				Table:   300,
				NextHop: net.IPv4(10, 1, 1, 1),
			}},
			Want: net.IPv4(2, 2, 2, 2),
		},
	}
	for _, test := range tests {
		m := &MockNetlink{routes: test.Routes}
		b := &MockBgpServer{}
		db := &MockDb{}
		manager := netlink.NewNetlinkManager(m, b, db)
		t.Run(test.Name, func(t *testing.T) {
			got, err := manager.DiscoverTunnelSource(test.TunnelDst)
			if err != nil && !test.ExpectError {
				t.Errorf("error: %v", err)
			}
			if err == nil && test.ExpectError {
				t.Errorf("wanted error but return nil")
			}
			if !test.Want.Equal(got) {
				t.Errorf("unexpected src address - got: %s, want: %s", got, test.Want)
			}
		})
	}
}

func TestNetlinkManager_CreateTunnel(t *testing.T) {
	tests := []struct {
		Name         string
		Description  string
		ExpectError  bool
		Tunnel       *netlink.Tunnel
		AddrsAdded   []string
		DoubleZeroIP net.IP
	}{
		{
			Name:        "tunnel_created_happy_path",
			Description: "tunnel creation is successful",
			ExpectError: false,
			Tunnel: &netlink.Tunnel{
				Name:           "doublezero0",
				EncapType:      netlink.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(10, 1, 1, 1),
				RemoteOverlay:  net.IPv4(10, 1, 1, 0),
			},
			AddrsAdded:   []string{"10.1.1.1/31", "10.0.0.0/32"},
			DoubleZeroIP: net.IP{10, 0, 0, 0},
		},
	}
	for _, test := range tests {
		m := &MockNetlink{}
		b := &MockBgpServer{}
		db := &MockDb{}
		manager := netlink.NewNetlinkManager(m, b, db)
		t.Run(test.Name, func(t *testing.T) {
			err := manager.CreateTunnelWithIP(test.Tunnel, test.DoubleZeroIP)
			if err != nil && !test.ExpectError {
				t.Errorf("error: %v", err)
			}
			if err == nil && test.ExpectError {
				t.Errorf("wanted error but returned nil")
			} else {
				// Make sure we added a tunnel interface
				if diff := cmp.Diff(test.Tunnel, m.tunAdded); diff != "" {
					t.Errorf("TunnelAdd mismatch (-want +got): %s\n", diff)
				}
				// Make sure we added tunnel interface and DZ addressing
				if diff := cmp.Diff(test.AddrsAdded, m.tunAddrAdded); diff != "" {
					t.Errorf("TunnelAddrAdd mismatch (-want +got): %s\n", diff)
				}
				// Make sure we set the tunnel to up
				if diff := cmp.Diff(test.Tunnel, m.tunUp); diff != "" {
					t.Errorf("TunnelUp mismatch (-want +got): %s\n", diff)
				}
			}
		})
	}
}

func TestNetlinkManager_CreateIPRules(t *testing.T) {
	tests := []struct {
		Name        string
		Description string
		ExpectError bool
		Prefixes    []*net.IPNet
		RulesAdded  []*netlink.IPRule
	}{
		{
			Name:        "rule_created_happy_path_single_prefix",
			Description: "add a set of ip rules for a single prefix",
			ExpectError: false,
			Prefixes: []*net.IPNet{
				{IP: net.IPv4(1, 1, 1, 1), Mask: []byte{255, 255, 255, 255}},
			},
			RulesAdded: []*netlink.IPRule{
				{
					Priority: 100,
					Table:    100,
					SrcNet:   &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
					DstNet:   &net.IPNet{IP: net.IPv4(1, 1, 1, 1), Mask: []byte{255, 255, 255, 255}},
				},
				{
					Priority: 101,
					Table:    101,
					SrcNet:   &net.IPNet{IP: net.IPv4(1, 1, 1, 1), Mask: []byte{255, 255, 255, 255}},
					DstNet:   &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
				},
			},
		},
		{
			Name:        "rule_created_happy_path_multiple_prefix",
			Description: "add a set of ip rules for multiple prefixes",
			ExpectError: false,
			Prefixes: []*net.IPNet{
				{IP: net.IPv4(1, 1, 1, 1), Mask: []byte{255, 255, 255, 255}},
				{IP: net.IPv4(100, 0, 0, 0), Mask: []byte{255, 255, 255, 0}},
			},
			RulesAdded: []*netlink.IPRule{
				{
					Priority: 100,
					Table:    100,
					SrcNet:   &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
					DstNet:   &net.IPNet{IP: net.IPv4(1, 1, 1, 1), Mask: []byte{255, 255, 255, 255}},
				},
				{
					Priority: 101,
					Table:    101,
					SrcNet:   &net.IPNet{IP: net.IPv4(1, 1, 1, 1), Mask: []byte{255, 255, 255, 255}},
					DstNet:   &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
				},
				{
					Priority: 100,
					Table:    100,
					SrcNet:   &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
					DstNet:   &net.IPNet{IP: net.IPv4(100, 0, 0, 0), Mask: []byte{255, 255, 255, 0}},
				},
				{
					Priority: 101,
					Table:    101,
					SrcNet:   &net.IPNet{IP: net.IPv4(100, 0, 0, 0), Mask: []byte{255, 255, 255, 0}},
					DstNet:   &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
				},
			},
		},
	}
	for _, test := range tests {
		m := &MockNetlink{}
		b := &MockBgpServer{}
		db := &MockDb{}
		manager := netlink.NewNetlinkManager(m, b, db)
		t.Run(test.Name, func(t *testing.T) {
			err := manager.CreateIPRules(test.Prefixes)
			if err != nil && !test.ExpectError {
				t.Errorf("error: %v", err)
			}
			if err == nil && test.ExpectError {
				t.Errorf("wanted error but returned nil")
			} else {
				if diff := cmp.Diff(test.RulesAdded, m.ruleAdded); diff != "" {
					t.Errorf("CreateIPRules mismatch (-want +got): %s\n", diff)
				}
			}
		})
	}
}

func TestNetlinkManager_Remove(t *testing.T) {

	t.Run("no_tunnel_provisioned", func(t *testing.T) {
		db := &MockDb{}
		m := &MockNetlink{}
		b := &MockBgpServer{}
		manager := netlink.NewNetlinkManager(m, b, db)
		if err := manager.Remove(); err != nil {
			t.Fatalf("expected nil but got err: %v", err)
		}
	})

	t.Run("successful_removal", func(t *testing.T) {
		root, err := os.MkdirTemp("", "doublezerod")
		if err != nil {
			t.Fatalf("error creating temp dir: %v", err)
		}
		defer os.RemoveAll(root)

		// XDG_STATE_HOME is used in NewDb so use it to set a tmp dir
		t.Setenv("XDG_STATE_HOME", root)

		path := filepath.Join(root, "doublezerod")
		if err := os.Mkdir(path, 0766); err != nil {
			t.Fatalf("error creating state dir: %v", err)
		}

		stateFile := filepath.Join(path, "doublezerod.json")
		// Create an empty file so we have something to delete
		err = netlink.WriteFile(stateFile, []byte{}, os.FileMode(os.O_RDWR|os.O_CREATE|os.O_TRUNC))
		if err != nil {
			t.Fatalf("could not create file: %v", err)
		}

		// add non-nil state to skip nil check
		db := &netlink.Db{State: &netlink.ProvisionRequest{}, Path: stateFile}
		m := &MockNetlink{}
		b := &MockBgpServer{}
		manager := netlink.NewNetlinkManager(m, b, db)
		manager.UnicastTunnel = &netlink.Tunnel{
			Name:           "doublezero0",
			EncapType:      netlink.GRE,
			LocalUnderlay:  net.IP{1, 1, 1, 1},
			RemoteUnderlay: net.IP{2, 2, 2, 2},
			LocalOverlay:   net.IP{169, 254, 0, 1},
			RemoteOverlay:  net.IP{169, 254, 0, 0},
		}
		manager.Rules = []*netlink.IPRule{
			{
				Priority: 100,
				Table:    100,
				SrcNet:   &net.IPNet{IP: net.IP{1, 1, 1, 0}, Mask: net.IPMask{255, 255, 255, 0}},
				DstNet:   &net.IPNet{IP: net.IP{0, 0, 0, 0}, Mask: net.IPMask{0, 0, 0, 0}},
			},
		}
		manager.Routes = []*netlink.Route{
			{
				Dst:     &net.IPNet{IP: net.IP{0, 0, 0, 0}, Mask: net.IPMask{0, 0, 0, 0}},
				Src:     net.IP{1, 1, 1, 0},
				Table:   100,
				NextHop: net.IP{169, 254, 0, 0},
			},
		}
		// we set these to nil on removal so we need to copy in the test
		wantRules := manager.Rules
		wantRoutes := manager.Routes
		wantTunnel := manager.UnicastTunnel
		if err := manager.Remove(); err != nil {
			t.Fatalf("error when removing tunnel config: %v", err)
		}
		// check peer is deleted
		if b.deletedPeer == nil {
			t.Fatalf("bgp peer never deleted")
		}
		// check rules are removed
		if diff := cmp.Diff(wantRules, m.ruleRemoved); diff != "" {
			t.Fatalf("removed rules mismatch (-want +got): %s\n", diff)
		}
		// check routes are removed
		if diff := cmp.Diff(wantRoutes, m.routesRemoved); diff != "" {
			t.Fatalf("removed routes mismatch (-want +got): %s\n", diff)
		}
		// check tunnel is removed
		if diff := cmp.Diff(wantTunnel, m.tunRemoved); diff != "" {
			t.Fatalf("removed tunnel mismatch (-want +got): %s\n", diff)
		}
		// check state file is removed
		if _, err := os.Stat(stateFile); err == nil {
			t.Fatalf("state file at %s still exists when should be removed", stateFile)
		}
		// check in-memory db state has been cleared
		if db.State != nil {
			t.Fatalf("db state should be nil")
		}
	})
}
