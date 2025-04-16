package netlink_test

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

	"github.com/google/go-cmp/cmp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/netlink"
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
		Want        *netlink.ProvisionRequest
		ExpectError bool
	}{
		{
			Name:    "unmarshal_provision_request_successfully",
			Request: &netlink.ProvisionRequest{},
			Data: []byte(
				`{
					"tunnel_src": "1.1.1.1",
					"tunnel_dst": "2.2.2.2",
					"tunnel_net": "10.0.0.0/31",
					"doublezero_ip": "3.3.3.3",
					"doublezero_prefixes": ["10.0.0.0/24"]
				}`,
			),
			Want: &netlink.ProvisionRequest{
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
			Validator: &netlink.ProvisionRequest{
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
	db := &MockDb{state: nil}
	manager := netlink.NewNetlinkManager(m, b, db)

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
	mux.HandleFunc("POST /provision", manager.ServeProvision)
	mux.HandleFunc("GET /status", manager.ServeStatus)

	opts := []api.Option{
		api.WithBaseContext(ctx),
		api.WithHandler(mux),
		api.WithSockFile(f.Name()),
	}
	api := api.NewApiServer(opts...)
	go func() {
		if err := api.Serve(lis); err != nil {
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
		want := `{"status": "disconnected"}`
		got, _ := io.ReadAll(resp.Body)
		if diff := cmp.Diff(want, string(got)); diff != "" {
			t.Fatalf("wrong response (-want +got): %s\n", diff)
		}
	})

	t.Run("provisioned_tunnel_status", func(t *testing.T) {
		db.state = &netlink.ProvisionRequest{
			TunnelSrc:    net.IP{1, 1, 1, 1},
			TunnelDst:    net.IP{2, 2, 2, 2},
			DoubleZeroIP: net.IP{3, 3, 3, 3},
			UserType:     netlink.UserTypeEdgeFiltering,
		}
		provisionBody := `{
					"tunnel_src": "1.1.1.1",
					"tunnel_dst": "2.2.2.2",
					"tunnel_net": "169.254.0.0/31",
					"doublezero_ip": "3.3.3.3",
					"doublezero_prefixes": ["3.0.0.0/24"],
					"user_type": "EdgeFiltering"
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
		want := `{"tunnel_name":"doublezero0","tunnel_src":"1.1.1.1","tunnel_dst":"2.2.2.2","doublezero_ip":"3.3.3.3","status":"connected"}` + "\n"
		got, _ := io.ReadAll(resp.Body)
		if diff := cmp.Diff(want, string(got)); diff != "" {
			t.Fatalf("Response body mismatch (-want +got): %s\n", diff)
		}
	})

}

func TestNetlinkManager_HttpEndpoints(t *testing.T) {
	m := &MockNetlink{}
	b := &MockBgpServer{}
	db := &MockDb{state: &netlink.ProvisionRequest{}}
	manager := netlink.NewNetlinkManager(m, b, db)

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
	mux.HandleFunc("POST /provision", manager.ServeProvision)
	mux.HandleFunc("POST /remove", manager.ServeRemove)
	mux.HandleFunc("GET /status", manager.ServeStatus)

	opts := []api.Option{
		api.WithBaseContext(ctx),
		api.WithHandler(mux),
		api.WithSockFile(f.Name()),
	}
	api := api.NewApiServer(opts...)
	go func() {
		if err := api.Serve(lis); err != nil {
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
		Tunnel      *netlink.Tunnel
		AddrsAdded  []string
		RulesAdded  []*netlink.IPRule
		RoutesAdded []*netlink.Route
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
			Tunnel: &netlink.Tunnel{
				Name:           "doublezero0",
				EncapType:      netlink.GRE,
				LocalUnderlay:  net.IPv4(1, 1, 1, 1),
				RemoteUnderlay: net.IPv4(2, 2, 2, 2),
				LocalOverlay:   net.IPv4(10, 1, 1, 1),
				RemoteOverlay:  net.IPv4(10, 1, 1, 0),
			},
			AddrsAdded: []string{"10.1.1.1/31", "10.0.0.0/32"},
			RulesAdded: []*netlink.IPRule{
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
			RoutesAdded: []*netlink.Route{
				{Src: net.IPv4(10, 0, 0, 0), Dst: &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: []byte{0, 0, 0, 0}}, Table: 101, NextHop: net.IPv4(10, 1, 1, 0)},
			},
			ExpectError: false,
		},
		{
			Name:        "remove_happy_path",
			Description: "successfully remove the tunnel",
			Endpoint:    "/remove",
			Method:      http.MethodPost,
			Body:        `{}`,
			Response:    `{"status": "ok"}`,
			Tunnel:      &netlink.Tunnel{},
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
		if test.Endpoint == "/remove" && len(manager.Rules) > 0 {
			t.Errorf("Call to remove did not remove rules from netlink manager: %v", manager.Rules)
		}
		if test.Endpoint == "/remove" && !slices.Contains(m.callLog, "RuleDel") {
			t.Errorf("Call to remove did not call Netlink.RuleDel: %v", m.callLog)
		}
		// Make sure /remove actually removes the routes
		if test.Endpoint == "/remove" && len(manager.Routes) > 0 {
			t.Errorf("Call to remove did not remove routes from netlink manager: %v", manager.Routes)
		}
		if test.Endpoint == "/remove" && !slices.Contains(m.callLog, "RouteDelete") {
			t.Errorf("Call to remove did not call Netlink.RouteDelete: %v", m.callLog)
		}
		// Make sure /remove actually removes the tunnels
		if test.Endpoint == "/remove" && manager.UnicastTunnel != nil {
			t.Errorf("Call to remove did not remove routes from netlink manager: %v", manager.Routes)
		}
		if test.Endpoint == "/remove" && !slices.Contains(m.callLog, "TunnelDelete") {
			t.Errorf("Call to remove did not call Netlink.TunnelDelete: %v", m.callLog)
		}

	}
}
