package manager_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/jwhited/corebgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/manager"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"golang.org/x/sys/unix"
)

type validator interface {
	Validate() error
}

func TestNetlinkManager_ProvisionRequestUnmarshal(t *testing.T) {
	tests := []struct {
		Name        string
		Description string
		Request     json.Unmarshaler
		Data        []byte
		Want        *api.ProvisionRequest
		ExpectError bool
	}{
		{
			Name:    "unmarshal_provision_request_successfully",
			Request: &api.ProvisionRequest{},
			Data: []byte(
				`{
					"tunnel_src": "1.1.1.1",
					"tunnel_dst": "2.2.2.2",
					"tunnel_net": "10.0.0.0/31",
					"doublezero_ip": "3.3.3.3",
					"doublezero_prefixes": ["10.0.0.0/24"]
				}`,
			),
			Want: &api.ProvisionRequest{
				TunnelSrc:          net.IPv4(1, 1, 1, 1),
				TunnelDst:          net.IPv4(2, 2, 2, 2),
				TunnelNet:          &net.IPNet{IP: net.IPv4(10, 0, 0, 0), Mask: []byte{255, 255, 255, 254}},
				DoubleZeroIP:       net.IPv4(3, 3, 3, 3),
				DoubleZeroPrefixes: []*net.IPNet{{IP: net.IPv4(10, 0, 0, 0), Mask: []byte{255, 255, 255, 0}}},
			},
			ExpectError: false,
		},
	}
	for _, test := range tests {
		err := json.Unmarshal(test.Data, test.Request)
		if err != nil && !test.ExpectError {
			t.Fatalf("error during test %s: %v", test.Name, err)
		}
		if err == nil && test.ExpectError {
			t.Errorf("wanted error but return nil")
		}
		if diff := cmp.Diff(test.Want, test.Request); diff != "" {
			t.Errorf("Unmarshal mismatch (-want +got): %s\n", diff)
		}
	}
}

func TestNetlinkManager_ProvisionRequestValidation(t *testing.T) {
	tests := []struct {
		Name        string
		Description string
		Validator   validator
		ExpectError bool
	}{
		{
			Name:        "valid_provision_req",
			Description: "make sure a valid provision request returns no error",
			Validator: &api.ProvisionRequest{
				TunnelSrc:          nil,
				TunnelDst:          net.IPv4(1, 1, 1, 1),
				TunnelNet:          &net.IPNet{IP: net.IPv4(10, 0, 0, 0), Mask: []byte{255, 255, 255, 254}},
				DoubleZeroIP:       net.IPv4(2, 2, 2, 2),
				DoubleZeroPrefixes: []*net.IPNet{{IP: net.IPv4(10, 0, 0, 0), Mask: []byte{255, 255, 255, 0}}},
			},
			ExpectError: false,
		},
	}
	for _, test := range tests {
		err := test.Validator.Validate()
		if err != nil && !test.ExpectError {
			t.Errorf("error: %v", err)
		}
		if err == nil && test.ExpectError {
			t.Errorf("wanted error but return nil")
		}
	}
}

func TestHttpStatus(t *testing.T) {
	m := &MockNetlink{}
	b := &MockBgpServer{}
	pim := &MockPIMServer{}
	heartbeat := &MockHeartbeatSender{}
	mgr := manager.NewNetlinkManager(m, b, pim, heartbeat)

	f, err := os.CreateTemp("/tmp", "doublezero.sock")
	if err != nil {
		t.Fatalf("error creating sock file: %v", err)
	}
	defer os.Remove(f.Name())
	_ = unix.Unlink(f.Name())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lis, err := net.Listen("unix", f.Name())
	if err != nil {
		t.Fatalf("error creating listener: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /provision", mgr.ServeProvision)
	mux.HandleFunc("GET /status", mgr.ServeStatus)

	opts := []api.Option{
		api.WithBaseContext(ctx),
		api.WithHandler(mux),
		api.WithSockFile(f.Name()),
	}
	apiServer := api.NewApiServer(opts...)
	go func() {
		if err := apiServer.Serve(lis); err != nil {
			t.Errorf("api error: %v", err)
		}
	}()

	client := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", f.Name())
			},
		},
	}
	t.Run("no_tunnel_status", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "http://localhost/status", nil)
		if err != nil {
			t.Fatalf("error creating request: %v\n", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("error during request: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("wanted 200 response; got %d\n", resp.StatusCode)
		}
		// this previously returned `{"doublezero_status": {"session_status": "disconnected"}}  but now returns []
		// want := "[]\n"
		want := `[{"doublezero_status": {"session_status": "disconnected"}}]`
		got, _ := io.ReadAll(resp.Body)
		if diff := cmp.Diff(want, string(got)); diff != "" {
			t.Fatalf("wrong response (-want +got): %s\n", diff)
		}
	})

	t.Run("provisioned_tunnel_status", func(t *testing.T) {
		provisionBody := `{
					"tunnel_src": "1.1.1.1",
					"tunnel_dst": "2.2.2.2",
					"tunnel_net": "169.254.0.0/31",
					"doublezero_ip": "3.3.3.3",
					"doublezero_prefixes": ["3.0.0.0/24"],
					"user_type": "IBRL"
				}`

		req, err := http.NewRequest(http.MethodPost, "http://localhost/provision", strings.NewReader(provisionBody))
		if err != nil {
			t.Fatalf("error creating request: %v\n", err)
		}
		_, err = client.Do(req)
		if err != nil {
			t.Fatalf("error during request: %v", err)
		}

		req, err = http.NewRequest(http.MethodGet, "http://localhost/status", nil)
		if err != nil {
			t.Fatalf("error creating request: %v\n", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("error during request: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("wanted 200 response; got %d", resp.StatusCode)
		}
		want := `[{"tunnel_name":"doublezero0","tunnel_src":"1.1.1.1","tunnel_dst":"2.2.2.2","doublezero_ip":"3.3.3.3","doublezero_status":{"session_status":"Pending BGP Session","last_session_update":0},"user_type":"IBRL"}]` + "\n"
		got, _ := io.ReadAll(resp.Body)
		if diff := cmp.Diff(want, string(got), cmpopts.IgnoreFields(bgp.Session{}, "LastSessionUpdate")); diff != "" {
			t.Fatalf("Response body mismatch (-want +got): %s\n", diff)
		}
	})
}

func TestNetlinkManager_HttpEndpoints(t *testing.T) {
	m := &MockNetlink{}
	b := &MockBgpServer{}
	pim := &MockPIMServer{}
	heartbeat := &MockHeartbeatSender{}
	mgr := manager.NewNetlinkManager(m, b, pim, heartbeat)

	f, err := os.CreateTemp("/tmp", "doublezero.sock")
	if err != nil {
		t.Fatalf("error creating sock file: %v", err)
	}
	defer os.Remove(f.Name())
	_ = unix.Unlink(f.Name())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lis, err := net.Listen("unix", f.Name())
	if err != nil {
		t.Fatalf("error creating listener: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /provision", mgr.ServeProvision)
	mux.HandleFunc("POST /remove", mgr.ServeRemove)
	mux.HandleFunc("GET /status", mgr.ServeStatus)

	opts := []api.Option{
		api.WithBaseContext(ctx),
		api.WithHandler(mux),
		api.WithSockFile(f.Name()),
	}
	apiSrv := api.NewApiServer(opts...)
	go func() {
		if err := apiSrv.Serve(lis); err != nil {
			t.Errorf("api error: %v", err)
		}
	}()

	client := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", f.Name())
			},
		},
	}
	tests := []struct {
		Name        string
		Description string
		Endpoint    string
		Method      string
		Body        string
		Response    string
		Tunnel      *routing.Tunnel
		AddrsAdded  []string
		RulesAdded  []*routing.IPRule
		RoutesAdded []*routing.Route
		ExpectError bool
	}{
		{
			Name:        "provision_happy_path",
			Description: "successfully provision a tunnel",
			Endpoint:    "/provision",
			Method:      http.MethodPost,
			Body: `{
					"tunnel_src": "1.1.1.1",
					"tunnel_dst": "2.2.2.2",
					"tunnel_net": "10.1.1.0/31",
					"doublezero_ip": "10.0.0.0",
					"doublezero_prefixes": ["10.0.0.0/24"],
					"user_type": "EdgeFiltering"
				}`,
			Response: `{"status": "ok"}`,
			Tunnel: &routing.Tunnel{
				Name:           "doublezero0",
				EncapType:      routing.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(10, 1, 1, 1),
				RemoteOverlay:  net.IPv4(10, 1, 1, 0),
				MTU:            routing.GREMTU,
			},
			AddrsAdded: []string{"10.1.1.1/31", "10.0.0.0/32"},
			RulesAdded: []*routing.IPRule{
				{
					Priority: 100,
					Table:    100,
					SrcNet:   &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
					DstNet:   &net.IPNet{IP: net.IPv4(10, 0, 0, 0), Mask: []byte{255, 255, 255, 0}},
				},
				{
					Priority: 101,
					Table:    101,
					SrcNet:   &net.IPNet{IP: net.IPv4(10, 0, 0, 0), Mask: []byte{255, 255, 255, 0}},
					DstNet:   &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}},
				},
			},
			RoutesAdded: []*routing.Route{
				{Src: net.IPv4(10, 0, 0, 0), Dst: &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}}, Table: 101, NextHop: net.IPv4(10, 1, 1, 0)},
			},
			ExpectError: false,
		},
		{
			Name:        "remove_happy_path",
			Description: "successfully remove the tunnel",
			Endpoint:    "/remove",
			Method:      http.MethodPost,
			Body:        `{"user_type": "EdgeFiltering"}`,
			Response:    `{"status": "ok"}`,
			Tunnel:      &routing.Tunnel{},
			ExpectError: false,
		},
	}
	for _, test := range tests {
		url, err := url.JoinPath("http://localhost/", test.Endpoint)
		if err != nil {
			t.Fatalf("error creating url: %v", err)
		}

		req, err := http.NewRequest(test.Method, url, strings.NewReader(test.Body))
		if err != nil {
			t.Fatalf("error creating request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("error during request: %v", err)
		}
		defer resp.Body.Close()

		buf, _ := io.ReadAll(resp.Body)
		if string(buf) != test.Response {
			t.Fatalf("wrong response: %s", string(buf))
		}
		// Make sure /provision added a tunnel interface
		if diff := cmp.Diff(test.Tunnel, m.tunAdded); test.Endpoint == "/provision" && diff != "" {
			t.Errorf("TunnelAdd mismatch (-want +got): %s\n", diff)
		}
		// Make sure /provision added tunnel interface and DZ addressing
		if diff := cmp.Diff(test.AddrsAdded, m.tunAddrAdded); test.Endpoint == "/provision" && diff != "" {
			t.Errorf("TunnelAddrAdd mismatch (-want +got): %s\n", diff)
		}
		// Make sure /provision set the tunnel to up
		if diff := cmp.Diff(test.Tunnel, m.tunUp); test.Endpoint == "/provision" && diff != "" {
			t.Errorf("TunnelUp mismatch (-want +got): %s\n", diff)
		}
		// Make sure /provision add the correct IP rules
		if diff := cmp.Diff(test.RulesAdded, m.ruleAdded); test.Endpoint == "/provision" && diff != "" {
			t.Errorf("CreateIPRules mismatch (-want +got): %s\n", diff)
		}
		// Make sure /provision add the correct routes
		if diff := cmp.Diff(test.RoutesAdded, m.routesAdded); test.Endpoint == "/provision" && diff != "" {
			t.Errorf("CreateIPRules mismatch (-want +got): %s\n", diff)
		}
		// Make sure /remove actually removes the rules
		if test.Endpoint == "/remove" && len(mgr.Rules) > 0 {
			t.Errorf("Call to remove did not remove rules from netlink manager: %v", mgr.Rules)
		}
		if test.Endpoint == "/remove" && !slices.Contains(m.callLog, "RuleDel") {
			t.Errorf("Call to remove did not call Netlink.RuleDel: %v", m.callLog)
		}
		// Make sure /remove actually removes the routes
		if test.Endpoint == "/remove" && len(mgr.Routes) > 0 {
			t.Errorf("Call to remove did not remove routes from netlink manager: %v", mgr.Routes)
		}
		if test.Endpoint == "/remove" && !slices.Contains(m.callLog, "RouteDelete") {
			t.Errorf("Call to remove did not call Netlink.RouteDelete: %v", m.callLog)
		}
		// TODO:   remove and/or fix this
		// Make sure /remove actually removes the tunnels
		// if test.Endpoint == "/remove" && manager.UnicastService != nil {
		// 	t.Errorf("Call to remove did not remove routes from netlink manager: %v", manager.Routes)
		// }
		if test.Endpoint == "/remove" && !slices.Contains(m.callLog, "TunnelDelete") {
			t.Errorf("Call to remove did not call Netlink.TunnelDelete: %v", m.callLog)
		}

	}
}

func TestNetlinkManager_GetProvisionedServices(t *testing.T) {
	t.Run("no_services", func(t *testing.T) {
		mgr := manager.NewNetlinkManager(&MockNetlink{}, &MockBgpServer{}, &MockPIMServer{}, &MockHeartbeatSender{})
		reqs := mgr.GetProvisionedServices()
		if len(reqs) != 0 {
			t.Fatalf("expected 0 provisioned services, got %d", len(reqs))
		}
	})

	t.Run("unicast_only", func(t *testing.T) {
		mgr := manager.NewNetlinkManager(&MockNetlink{}, &MockBgpServer{}, &MockPIMServer{}, &MockHeartbeatSender{})
		pr := api.ProvisionRequest{
			UserType:           api.UserTypeIBRL,
			TunnelSrc:          net.IPv4(1, 1, 1, 1),
			TunnelDst:          net.IPv4(2, 2, 2, 2),
			TunnelNet:          &net.IPNet{IP: net.IPv4(169, 254, 0, 0), Mask: []byte{255, 255, 255, 254}},
			DoubleZeroIP:       net.IPv4(10, 0, 0, 1),
			DoubleZeroPrefixes: []*net.IPNet{{IP: net.IPv4(10, 0, 0, 0), Mask: []byte{255, 255, 255, 0}}},
		}
		if err := mgr.Provision(pr); err != nil {
			t.Fatalf("provision error: %v", err)
		}

		reqs := mgr.GetProvisionedServices()
		if len(reqs) != 1 {
			t.Fatalf("expected 1 provisioned service, got %d", len(reqs))
		}
		if reqs[0].UserType != api.UserTypeIBRL {
			t.Fatalf("expected UserTypeIBRL, got %v", reqs[0].UserType)
		}
	})

	t.Run("after_remove", func(t *testing.T) {
		mgr := manager.NewNetlinkManager(&MockNetlink{}, &MockBgpServer{}, &MockPIMServer{}, &MockHeartbeatSender{})
		pr := api.ProvisionRequest{
			UserType:           api.UserTypeIBRL,
			TunnelSrc:          net.IPv4(1, 1, 1, 1),
			TunnelDst:          net.IPv4(2, 2, 2, 2),
			TunnelNet:          &net.IPNet{IP: net.IPv4(169, 254, 0, 0), Mask: []byte{255, 255, 255, 254}},
			DoubleZeroIP:       net.IPv4(10, 0, 0, 1),
			DoubleZeroPrefixes: []*net.IPNet{{IP: net.IPv4(10, 0, 0, 0), Mask: []byte{255, 255, 255, 0}}},
		}
		if err := mgr.Provision(pr); err != nil {
			t.Fatalf("provision error: %v", err)
		}
		if err := mgr.Remove(api.UserTypeIBRL); err != nil {
			t.Fatalf("remove error: %v", err)
		}

		reqs := mgr.GetProvisionedServices()
		if len(reqs) != 0 {
			t.Fatalf("expected 0 provisioned services after remove, got %d", len(reqs))
		}
	})
}

type MockPIMServer struct{}

func (m *MockPIMServer) Start(conn pim.RawConner, iface string, tunnelAddr net.IP, group []net.IP) error {
	return nil
}

func (m *MockPIMServer) UpdateGroups(groups []net.IP) error {
	return nil
}

func (m *MockPIMServer) Close() error {
	return nil
}

type MockHeartbeatSender struct{}

func (m *MockHeartbeatSender) Start(iface string, srcIP net.IP, groups []net.IP, ttl int, interval time.Duration) error {
	return nil
}

func (m *MockHeartbeatSender) UpdateGroups(groups []net.IP) error {
	return nil
}

func (m *MockHeartbeatSender) Close() error {
	return nil
}

type MockBgpServer struct {
	deletedPeer net.IP
}

func (m *MockBgpServer) Serve(lis []net.Listener) error                   { return nil }
func (m *MockBgpServer) AddPeer(p *bgp.PeerConfig, nlri []bgp.NLRI) error { return nil }
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
	tunAddrAdded  []string
	tunUp         *routing.Tunnel
	ruleAdded     []*routing.IPRule
	ruleRemoved   []*routing.IPRule
	callLog       []string
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

func (m *MockNetlink) TunnelAddrAdd(t *routing.Tunnel, ip string, scope int) error {
	m.tunAddrAdded = append(m.tunAddrAdded, ip)
	return nil
}

func (m *MockNetlink) TunnelUp(t *routing.Tunnel) error {
	m.tunUp = t
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
