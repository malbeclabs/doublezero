package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/netlink"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/runtime"

	nl "github.com/vishvananda/netlink"
)

func TestEndToEnd_IBRL(t *testing.T) {
	rootPath, err := os.MkdirTemp("", "doublezerod")
	if err != nil {
		t.Fatalf("error creating temp dir: %v", err)
	}
	defer os.RemoveAll(rootPath)

	t.Setenv("XDG_STATE_HOME", rootPath)

	path := filepath.Join(rootPath, "doublezerod")
	if err := os.Mkdir(path, 0766); err != nil {
		t.Fatalf("error creating state dir: %v", err)
	}
	tests := []struct {
		name                string
		userType            netlink.UserType
		provisioningRequest map[string]string
		goldenStateFile     string
	}{
		{
			name:     "test_ibrl",
			userType: netlink.UserTypeIBRL,
			provisioningRequest: map[string]string{

				"tunnel_src":    "1.1.1.1",
				"tunnel_dst":    "2.2.2.2",
				"tunnel_net":    "169.254.0.0/31",
				"doublezero_ip": "1.1.1.1",
				"user_type":     "ibrl",
			},
			goldenStateFile: "./fixtures/doublezerod.ibrl.json",
		},
		{
			name:     "test_ibrl_with_allocated_ip",
			userType: netlink.UserTypeIBRLWithAllocatedIP,
			provisioningRequest: map[string]string{

				"tunnel_src":    "1.1.1.1",
				"tunnel_dst":    "2.2.2.2",
				"tunnel_net":    "169.254.0.0/31",
				"doublezero_ip": "3.3.3.3",
				"user_type":     "ibrl_with_allocated_ip",
			},
			goldenStateFile: "./fixtures/doublezerod.ibrl.with.allocated.ip.json",
		},
	}
	for _, test := range tests {
		errChan := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		t.Run(test.name, func(t *testing.T) {
			sockFile := filepath.Join(rootPath, "doublezerod.sock")
			go func() {
				programId := ""
				err := runtime.Run(ctx, sockFile, false, programId, "")
				errChan <- err
			}()

			httpClient := http.Client{
				Transport: &http.Transport{
					DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
						return net.Dial("unix", sockFile)
					},
				},
			}

			t.Run("start_runtime", func(t *testing.T) {
				select {
				case err := <-errChan:
					if err != nil {
						t.Fatalf("error starting runtime: %v", err)
					}
				case <-time.After(5 * time.Second):
				}
			})

			t.Run("send_provision_request", func(t *testing.T) {
				url, err := url.JoinPath("http://localhost/", "provision")
				if err != nil {
					t.Fatalf("error creating url: %v", err)
				}
				body, _ := json.Marshal(test.provisioningRequest)
				req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(body))
				if err != nil {
					t.Fatalf("error creating request: %v", err)
				}
				resp, err := httpClient.Do(req)
				if err != nil {
					t.Fatalf("error during request: %v", err)
				}
				defer resp.Body.Close()

				got, _ := io.ReadAll(resp.Body)
				want := `{"status": "ok"}`
				if string(got) != want {
					t.Fatalf("wrong response: %s", string(got))
				}
			})

			t.Run("verify_tunnel_is_up", func(t *testing.T) {
				tun, err := nl.LinkByName("doublezero0")
				if err != nil {
					t.Fatalf("error fetching tunnel status: %v", err)
				}
				if tun.Attrs().Name != "doublezero0" {
					t.Fatalf("tunnel name is not doublezero0: %s", tun.Attrs().Name)
				}
				if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
					t.Fatalf("tunnel is not set to up state (6), got %d", tun.Attrs().OperState)
				}
				if tun.Attrs().MTU != 1476 {
					t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
				}
			})

			t.Run("verify_state_file_is_created", func(t *testing.T) {
				got, err := os.ReadFile(filepath.Join(rootPath, "doublezerod", "doublezerod.json"))
				if err != nil {
					t.Fatalf("error reading state file: %v", err)
				}
				want, err := os.ReadFile(test.goldenStateFile)
				if err != nil {
					t.Fatalf("error reading state file: %v", err)
				}
				if diff := cmp.Diff(string(want), string(got)); diff != "" {
					t.Fatalf("State mismatch (-want +got): %s\n", diff)
				}
			})

			t.Run("stop_runtime", func(t *testing.T) {
				cancel()
				select {
				case err := <-errChan:
					if err != nil {
						t.Fatalf("error stopping runtime: %v", err)
					}
				case <-time.After(5 * time.Second):
					log.Fatalf("timed out waiting for close")
				}
			})

			ctx, cancel = context.WithCancel(context.Background())
			go func() {
				programId := ""
				err := runtime.Run(ctx, sockFile, false, programId, "")
				errChan <- err
			}()

			<-time.After(5 * time.Second)

			t.Run("restart_runtime", func(t *testing.T) {
				select {
				case err := <-errChan:
					if err != nil {
						t.Fatalf("error starting runtime: %v", err)
					}
				case <-time.After(5 * time.Second):
				}
			})

			t.Run("state_recovery_verify_tunnel_is_up", func(t *testing.T) {
				tun, err := nl.LinkByName("doublezero0")
				if err != nil {
					t.Fatalf("error fetching tunnel status: %v", err)
				}
				if tun.Attrs().Name != "doublezero0" {
					t.Fatalf("tunnel name is not doublezero0: %s", tun.Attrs().Name)
				}
				if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
					t.Fatalf("tunnel is not set to up state (6), got %d", tun.Attrs().OperState)
				}
				if tun.Attrs().MTU != 1476 {
					t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
				}
			})

			t.Run("send_remove_request", func(t *testing.T) {
				url, err := url.JoinPath("http://localhost/", "remove")
				if err != nil {
					t.Fatalf("error creating url: %v", err)
				}
				req, err := http.NewRequest(http.MethodPost, url, nil)
				if err != nil {
					t.Fatalf("error creating request: %v", err)
				}
				resp, err := httpClient.Do(req)
				if err != nil {
					t.Fatalf("error during request: %v", err)
				}
				defer resp.Body.Close()

				got, _ := io.ReadAll(resp.Body)
				want := `{"status": "ok"}`
				if string(got) != want {
					t.Fatalf("wrong response: %s", string(got))
				}
			})

			t.Run("verify_tunnel_is_removed", func(t *testing.T) {
				_, err := nl.LinkByName("doublezero0")
				if !errors.As(err, &nl.LinkNotFoundError{}) {
					t.Fatalf("expected LinkNotFoundError; got: %v", err)
				}
			})

			t.Run("state_removal_stop_runtime", func(t *testing.T) {
				cancel()
				select {
				case err := <-errChan:
					if err != nil {
						t.Fatalf("error stopping runtime: %v", err)
					}
				case <-time.After(5 * time.Second):
					log.Fatalf("timed out waiting for close")
				}
			})

			t.Run("state_removal_verify_state_file_removed", func(t *testing.T) {
				if _, err := os.Stat(filepath.Join(rootPath, "doublezerod", "doublezerod.json")); err == nil {
					t.Fatalf("state file still exists when should be removed")
				}
			})
		})
	}
}

// TestEndToEnd_EdgeFiltering exercises the entire client daemon end to end. It starts
// the runtime, makes a provisioning http call, verifies netlink state has
// been created as well as the statefile. The daemon is then restarted to verify
// successful recovery via the statefile.
// The test then tears down the state via the remove http endpoint and verifies
// the tunnel, ip rules, routes as well as the statefile have been successfully
// removed.
func TestEndToEnd_EdgeFiltering(t *testing.T) {
	errChan := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())

	rootPath, err := os.MkdirTemp("", "doublezerod")
	if err != nil {
		t.Fatalf("error creating temp dir: %v", err)
	}
	defer os.RemoveAll(rootPath)

	t.Setenv("XDG_STATE_HOME", rootPath)

	path := filepath.Join(rootPath, "doublezerod")
	if err := os.Mkdir(path, 0766); err != nil {
		t.Fatalf("error creating state dir: %v", err)
	}

	sockFile := filepath.Join(rootPath, "doublezerod.sock")
	go func() {
		programId := ""
		err := runtime.Run(ctx, sockFile, false, programId, "")
		errChan <- err
	}()

	httpClient := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sockFile)
			},
		},
	}

	// case: clean start and provision tunnel
	t.Run("start_runtime", func(t *testing.T) {
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error starting runtime: %v", err)
			}
		case <-time.After(5 * time.Second):
		}
	})

	t.Run("send_provision_request", func(t *testing.T) {
		url, err := url.JoinPath("http://localhost/", "provision")
		if err != nil {
			t.Fatalf("error creating url: %v", err)
		}
		body := `{
					"tunnel_src": "1.1.1.1",
					"tunnel_dst": "2.2.2.2",
					"tunnel_net": "169.254.0.0/31",
					"doublezero_ip": "3.3.3.3",
					"doublezero_prefixes": ["3.0.0.0/24"],
					"user_type": "edge_filtering"
				}`
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		if err != nil {
			t.Fatalf("error creating request: %v", err)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("error during request: %v", err)
		}
		defer resp.Body.Close()

		got, _ := io.ReadAll(resp.Body)
		want := `{"status": "ok"}`
		if string(got) != want {
			t.Fatalf("wrong response: %s", string(got))
		}
	})

	t.Run("verify_tunnel_is_up", func(t *testing.T) {
		tun, err := nl.LinkByName("doublezero0")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		if tun.Attrs().Name != "doublezero0" {
			t.Fatalf("tunnel name is not doublezero0: %s", tun.Attrs().Name)
		}
		if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
			t.Fatalf("tunnel is not set to up state (6), got %d", tun.Attrs().OperState)
		}
		if tun.Attrs().MTU != 1476 {
			t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
		}
	})

	t.Run("verify_ip_rules_created", func(t *testing.T) {
		// ip rule 100: from all to 3.0.0.0/24 table 100
		rules, err := nl.RuleListFiltered(0, &nl.Rule{Priority: 100}, nl.RT_FILTER_PRIORITY)
		if err != nil {
			t.Fatalf("error fetching ip rules: %v", err)
		}
		if rules[0].Src != nil {
			t.Fatalf("rule 100 should be sourced from all; got %s", rules[0].Src)
		}
		if rules[0].Dst.String() != "3.0.0.0/24" {
			t.Fatalf("rule 100 should be destined to 3.0.0.0/24; got %s", rules[0].Dst)
		}
		if rules[0].Table != 100 {
			t.Fatalf("rule 100 should be looked up in table 100; got %d", rules[0].Table)
		}
		// ip rule 100: from 3.0.0.0/24 to all table 101
		rules, err = nl.RuleListFiltered(0, &nl.Rule{Priority: 101}, nl.RT_FILTER_PRIORITY)
		if err != nil {
			t.Fatalf("error fetching ip rules: %v", err)
		}
		if rules[0].Src.String() != "3.0.0.0/24" {
			t.Fatalf("rule 101 should be sourced from 3.0.0.0")
		}
		if rules[0].Dst != nil {
			t.Fatalf("rule 101 should be destined too all; got %s", rules[0].Dst)
		}
		if rules[0].Table != 101 {
			t.Fatalf("rule 100 should be looked up in table 100; got %d", rules[0].Table)
		}
	})

	t.Run("verify_default_route_created", func(t *testing.T) {
		route, err := nl.RouteListFiltered(0, &nl.Route{Table: 101}, nl.RT_FILTER_TABLE)
		if err != nil {
			t.Fatalf("error fetching routes: %v", err)
		}
		if !route[0].Src.Equal(net.IP{3, 3, 3, 3}) {
			t.Fatalf("route src hint should be 3.3.3.3; got %s", route[0].Src)
		}
		if route[0].Dst.String() != "0.0.0.0/0" {
			t.Fatalf("route dst should be 0.0.0.0/0; got %s", route[0].Dst)
		}
		if !route[0].Gw.Equal(net.IP{169, 254, 0, 0}) {
			t.Fatalf("route gw should be 169.254.0.0; got %s", route[0].Gw)
		}
	})

	// TODO: verify specific routes are created; this needs namespaces

	t.Run("verify_state_file_is_created", func(t *testing.T) {
		got, err := os.ReadFile(filepath.Join(rootPath, "doublezerod", "doublezerod.json"))
		if err != nil {
			t.Fatalf("error reading state file: %v", err)
		}
		want, err := os.ReadFile("./fixtures/doublezerod.edgefiltering.json")
		if err != nil {
			t.Fatalf("error reading state file: %v", err)
		}
		if diff := cmp.Diff(string(want), string(got)); diff != "" {
			t.Fatalf("State mismatch (-want +got): %s\n", diff)
		}
	})

	// case: restart and auto-recover state
	t.Run("stop_runtime", func(t *testing.T) {
		cancel()
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error stopping runtime: %v", err)
			}
		case <-time.After(5 * time.Second):
			log.Fatalf("timed out waiting for close")
		}
	})

	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		programId := ""
		err := runtime.Run(ctx, sockFile, false, programId, "")
		errChan <- err
	}()

	<-time.After(5 * time.Second)

	t.Run("restart_runtime", func(t *testing.T) {
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error starting runtime: %v", err)
			}
		case <-time.After(5 * time.Second):
		}
	})

	t.Run("state_recovery_verify_tunnel_is_up", func(t *testing.T) {
		tun, err := nl.LinkByName("doublezero0")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		if tun.Attrs().Name != "doublezero0" {
			t.Fatalf("tunnel name is not doublezero0: %s", tun.Attrs().Name)
		}
		if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
			t.Fatalf("tunnel is not set to up state (6), got %d", tun.Attrs().OperState)
		}
		if tun.Attrs().MTU != 1476 {
			t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
		}
	})

	t.Run("state_recovery_verify_ip_rules_created", func(t *testing.T) {
		// ip rule 100: from all to 3.0.0.0/24 table 100
		rules, err := nl.RuleListFiltered(0, &nl.Rule{Priority: 100}, nl.RT_FILTER_PRIORITY)
		if err != nil {
			t.Fatalf("error fetching ip rules: %v", err)
		}
		if rules[0].Src != nil {
			t.Fatalf("rule 100 should be sourced from all; got %s", rules[0].Src)
		}
		if rules[0].Dst.String() != "3.0.0.0/24" {
			t.Fatalf("rule 100 should be destined to 3.0.0.0/24; got %s", rules[0].Dst)
		}
		if rules[0].Table != 100 {
			t.Fatalf("rule 100 should be looked up in table 100; got %d", rules[0].Table)
		}
		// ip rule 100: from 3.0.0.0/24 to all table 101
		rules, err = nl.RuleListFiltered(0, &nl.Rule{Priority: 101}, nl.RT_FILTER_PRIORITY)
		if err != nil {
			t.Fatalf("error fetching ip rules: %v", err)
		}
		if rules[0].Src.String() != "3.0.0.0/24" {
			t.Fatalf("rule 101 should be sourced from 3.0.0.0")
		}
		if rules[0].Dst != nil {
			t.Fatalf("rule 101 should be destined too all; got %s", rules[0].Dst)
		}
		if rules[0].Table != 101 {
			t.Fatalf("rule 100 should be looked up in table 100; got %d", rules[0].Table)
		}
	})

	t.Run("state_recovery_verify_default_route_created", func(t *testing.T) {
		route, err := nl.RouteListFiltered(0, &nl.Route{Table: 101}, nl.RT_FILTER_TABLE)
		if err != nil {
			t.Fatalf("error fetching routes: %v", err)
		}
		if !route[0].Src.Equal(net.IP{3, 3, 3, 3}) {
			t.Fatalf("route src hint should be 3.3.3.3; got %s", route[0].Src)
		}
		if route[0].Dst.String() != "0.0.0.0/0" {
			t.Fatalf("route dst should be 0.0.0.0/0; got %s", route[0].Dst)
		}
		if !route[0].Gw.Equal(net.IP{169, 254, 0, 0}) {
			t.Fatalf("route gw should be 169.254.0.0; got %s", route[0].Gw)
		}
	})
	// TODO: verify specific routes are created; this needs namespaces

	// case: remove tunnel
	t.Run("send_remove_request", func(t *testing.T) {
		url, err := url.JoinPath("http://localhost/", "remove")
		if err != nil {
			t.Fatalf("error creating url: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			t.Fatalf("error creating request: %v", err)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("error during request: %v", err)
		}
		defer resp.Body.Close()

		got, _ := io.ReadAll(resp.Body)
		want := `{"status": "ok"}`
		if string(got) != want {
			t.Fatalf("wrong response: %s", string(got))
		}
	})

	t.Run("verify_tunnel_is_removed", func(t *testing.T) {
		_, err := nl.LinkByName("doublezero0")
		if !errors.As(err, &nl.LinkNotFoundError{}) {
			t.Fatalf("expected LinkNotFoundError; got: %v", err)
		}
	})

	t.Run("verify_ip_rules_are_removed", func(t *testing.T) {
		// ip rule 100: from all to 3.0.0.0/24 table 100
		rules, err := nl.RuleListFiltered(0, &nl.Rule{Priority: 100}, nl.RT_FILTER_PRIORITY)
		if err != nil {
			t.Fatalf("error fetching priority 100 rules: %v", err)
		}
		if len(rules) != 0 {
			t.Fatalf("wanted 0 rules found at priority 100; got %d: %+v", len(rules), rules)
		}
		// ip rule 100: from 3.0.0.0/24 to all table 101
		rules, err = nl.RuleListFiltered(0, &nl.Rule{Priority: 101}, nl.RT_FILTER_PRIORITY)
		if err != nil {
			t.Fatalf("error fetching priority 101 rules: %v", err)
		}
		if len(rules) != 0 {
			t.Fatalf("wanted 0 rules found at priority 101; got %d: %+v", len(rules), rules)
		}
	})

	t.Run("verify_default_route_is_removed", func(t *testing.T) {
		route, err := nl.RouteListFiltered(0, &nl.Route{Table: 101}, nl.RT_FILTER_TABLE)
		if err != nil {
			t.Fatalf("error fetching routes: %v", err)
		}
		if len(route) != 0 {
			t.Fatalf("wanted 0 routes found in table 101; got %d: %+v", len(route), route)
		}
	})

	// TODO: verify specific routes are removed; this needs namespaces

	t.Run("state_removal_stop_runtime", func(t *testing.T) {
		cancel()
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error stopping runtime: %v", err)
			}
		case <-time.After(5 * time.Second):
			log.Fatalf("timed out waiting for close")
		}
	})

	t.Run("state_removal_verify_state_file_removed", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(rootPath, "doublezerod", "doublezerod.json")); err == nil {
			t.Fatalf("state file still exists when should be removed")
		}
	})

	// case: latency endpoint
	// TODO: call latency endpoint
	// TODO: verify latency samples are returned
}
