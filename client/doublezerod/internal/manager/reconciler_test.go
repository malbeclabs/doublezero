package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

// --- test mocks ---

type mockFetcher struct {
	mu    sync.Mutex
	data  *serviceability.ProgramData
	err   error
	calls int
}

func (m *mockFetcher) GetProgramData(_ context.Context) (*serviceability.ProgramData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return m.data, m.err
}

func (m *mockFetcher) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

type mockNetlink struct {
	routes []*routing.Route
}

func (m *mockNetlink) TunnelAdd(*routing.Tunnel) error                  { return nil }
func (m *mockNetlink) TunnelDelete(*routing.Tunnel) error               { return nil }
func (m *mockNetlink) TunnelAddrAdd(*routing.Tunnel, string, int) error { return nil }
func (m *mockNetlink) TunnelUp(*routing.Tunnel) error                   { return nil }
func (m *mockNetlink) RouteAdd(*routing.Route) error                    { return nil }
func (m *mockNetlink) RouteDelete(*routing.Route) error                 { return nil }
func (m *mockNetlink) RouteGet(net.IP) ([]*routing.Route, error)        { return m.routes, nil }
func (m *mockNetlink) RuleAdd(*routing.IPRule) error                    { return nil }
func (m *mockNetlink) RuleDel(*routing.IPRule) error                    { return nil }
func (m *mockNetlink) RouteByProtocol(int) ([]*routing.Route, error) {
	return m.routes, nil
}

type mockBgpServer struct{}

func (m *mockBgpServer) Serve([]net.Listener) error                { return nil }
func (m *mockBgpServer) AddPeer(*bgp.PeerConfig, []bgp.NLRI) error { return nil }
func (m *mockBgpServer) DeletePeer(net.IP) error                   { return nil }
func (m *mockBgpServer) GetPeerStatus(net.IP) bgp.Session          { return bgp.Session{} }

type mockPIMServer struct{}

func (m *mockPIMServer) Start(pim.RawConner, string, net.IP, []net.IP) error { return nil }
func (m *mockPIMServer) Close() error                                        { return nil }

type mockHeartbeatSender struct{}

func (m *mockHeartbeatSender) Start(string, net.IP, []net.IP, int, time.Duration) error {
	return nil
}
func (m *mockHeartbeatSender) Close() error { return nil }

// --- test helpers ---

func newTestNLM(fetcher Fetcher, opts ...Option) *NetlinkManager {
	nl := &mockNetlink{}
	bgpSrv := &mockBgpServer{}
	pimSrv := &mockPIMServer{}
	hb := &mockHeartbeatSender{}
	return NewNetlinkManager(nl, bgpSrv, pimSrv, hb, append([]Option{WithFetcher(fetcher)}, opts...)...)
}

func newTestNLMWithNetlink(nl routing.Netlinker, fetcher Fetcher, opts ...Option) *NetlinkManager {
	bgpSrv := &mockBgpServer{}
	pimSrv := &mockPIMServer{}
	hb := &mockHeartbeatSender{}
	return NewNetlinkManager(nl, bgpSrv, pimSrv, hb, append([]Option{WithFetcher(fetcher)}, opts...)...)
}

func testDevice(pk [32]byte, ip [4]uint8, prefixes [][5]uint8) serviceability.Device {
	return serviceability.Device{
		PubKey:     pk,
		PublicIp:   ip,
		DzPrefixes: prefixes,
	}
}

func testUser(clientIP [4]uint8, devicePK [32]byte, userType serviceability.UserUserType, status serviceability.UserStatus) serviceability.User {
	return serviceability.User{
		ClientIp:     clientIP,
		DevicePubKey: devicePK,
		UserType:     userType,
		Status:       status,
		DzIp:         [4]uint8{10, 0, 0, 1},
		TunnelNet:    [5]uint8{10, 1, 0, 0, 31},
	}
}

func testConfig() serviceability.Config {
	return serviceability.Config{
		Local_asn:  65000,
		Remote_asn: 65001,
	}
}

// --- reconcile tests ---

func TestReconcile_ProvisionUnicast(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.UnicastService == nil {
		t.Fatal("expected unicast service to be provisioned")
	}
	pr := n.UnicastService.ProvisionRequest()
	if pr.UserType != api.UserTypeIBRL {
		t.Fatalf("expected UserTypeIBRL, got %v", pr.UserType)
	}
	if !pr.TunnelDst.Equal(net.IPv4(5, 6, 7, 8)) {
		t.Fatalf("expected tunnel dst 5.6.7.8, got %v", pr.TunnelDst)
	}
	if pr.BgpLocalAsn != 65000 {
		t.Fatalf("expected bgp local asn 65000, got %d", pr.BgpLocalAsn)
	}
}

func TestReconcile_ProvisionUnicast_WithTunnelEndpoint(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	user := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)
	user.TunnelEndpoint = [4]uint8{10, 0, 0, 99}

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{user},
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.UnicastService == nil {
		t.Fatal("expected unicast service to be provisioned")
	}
	pr := n.UnicastService.ProvisionRequest()
	if !pr.TunnelDst.Equal(net.IPv4(10, 0, 0, 99)) {
		t.Fatalf("expected tunnel dst 10.0.0.99 (from TunnelEndpoint), got %v", pr.TunnelDst)
	}
}

func TestReconcile_RemoveUnicast(t *testing.T) {
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config: testConfig(),
			Users:  []serviceability.User{}, // no matching users
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	// Pre-provision a unicast service so reconcile will remove it
	provisionUnicast(t, n)

	n.reconcile(context.Background())

	if n.UnicastService != nil {
		t.Fatal("expected unicast service to be removed")
	}
}

func TestReconcile_NoopWhenAlreadyProvisioned(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	// Let the reconciler do the initial provision so state matches.
	n.reconcile(context.Background())
	origSvc := n.UnicastService
	if origSvc == nil {
		t.Fatal("expected unicast service to be provisioned")
	}

	// Second reconcile with identical state — service should be unchanged.
	n.reconcile(context.Background())

	if n.UnicastService != origSvc {
		t.Fatal("expected service to remain unchanged (noop)")
	}
}

func TestReconcile_NoopWhenNoMatchingUsers(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users:   []serviceability.User{testUser([4]uint8{9, 9, 9, 9}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.UnicastService != nil {
		t.Fatal("expected no unicast service")
	}
	if n.MulticastService != nil {
		t.Fatal("expected no multicast service")
	}
}

func TestReconcile_IgnoresNonActivatedStatuses(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	statuses := []serviceability.UserStatus{
		serviceability.UserStatusPending,
		serviceability.UserStatusSuspendedDeprecated,
		serviceability.UserStatusDeleted,
		serviceability.UserStatusRejected,
		serviceability.UserStatusPendingBan,
		serviceability.UserStatusBanned,
		serviceability.UserStatusUpdating,
	}

	for _, status := range statuses {
		fetcher := &mockFetcher{
			data: &serviceability.ProgramData{
				Config:  testConfig(),
				Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
				Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, status)},
			},
		}

		n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
		n.reconcile(context.Background())

		if n.UnicastService != nil {
			t.Fatalf("status %d: expected no provision", status)
		}
	}
}

func TestReconcile_ProvisionMulticast(t *testing.T) {
	devicePK := [32]byte{1}
	mcastGroupPK := [32]byte{2}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	user := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeMulticast, serviceability.UserStatusActivated)
	user.Subscribers = [][32]uint8{mcastGroupPK}

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:          testConfig(),
			Devices:         []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users:           []serviceability.User{user},
			MulticastGroups: []serviceability.MulticastGroup{{PubKey: mcastGroupPK, MulticastIp: [4]uint8{239, 0, 0, 1}}},
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.MulticastService == nil {
		t.Fatal("expected multicast service to be provisioned")
	}
	pr := n.MulticastService.ProvisionRequest()
	if pr.UserType != api.UserTypeMulticast {
		t.Fatalf("expected UserTypeMulticast, got %v", pr.UserType)
	}
	if len(pr.MulticastSubGroups) != 1 {
		t.Fatalf("expected 1 sub group, got %d", len(pr.MulticastSubGroups))
	}
	if !pr.MulticastSubGroups[0].Equal(net.IPv4(239, 0, 0, 1)) {
		t.Fatalf("expected sub group 239.0.0.1, got %v", pr.MulticastSubGroups[0])
	}
}

func TestReconcile_RemoveMulticast(t *testing.T) {
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config: testConfig(),
			Users:  []serviceability.User{},
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	// Pre-provision a multicast service
	provisionMulticast(t, n)

	n.reconcile(context.Background())

	if n.MulticastService != nil {
		t.Fatal("expected multicast service to be removed")
	}
}

func TestReconcile_ReProvisionOnMulticastGroupChange(t *testing.T) {
	devicePK := [32]byte{1}
	mcastGroupPK1 := [32]byte{2}
	mcastGroupPK2 := [32]byte{3}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	// Start with one subscriber group.
	user := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeMulticast, serviceability.UserStatusActivated)
	user.Subscribers = [][32]uint8{mcastGroupPK1}

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users:   []serviceability.User{user},
			MulticastGroups: []serviceability.MulticastGroup{
				{PubKey: mcastGroupPK1, MulticastIp: [4]uint8{239, 0, 0, 1}},
				{PubKey: mcastGroupPK2, MulticastIp: [4]uint8{239, 0, 0, 2}},
			},
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.MulticastService == nil {
		t.Fatal("expected multicast service to be provisioned")
	}
	pr := n.MulticastService.ProvisionRequest()
	if len(pr.MulticastSubGroups) != 1 {
		t.Fatalf("expected 1 sub group after first reconcile, got %d", len(pr.MulticastSubGroups))
	}

	// Simulate a second group being added onchain (e.g. CLI subscribes to mg02).
	user.Subscribers = [][32]uint8{mcastGroupPK1, mcastGroupPK2}
	fetcher.data.Users = []serviceability.User{user}

	n.reconcile(context.Background())

	if n.MulticastService == nil {
		t.Fatal("expected multicast service to still be provisioned after re-provision")
	}
	pr = n.MulticastService.ProvisionRequest()
	if len(pr.MulticastSubGroups) != 2 {
		t.Fatalf("expected 2 sub groups after re-provision, got %d", len(pr.MulticastSubGroups))
	}
	if !pr.MulticastSubGroups[0].Equal(net.IPv4(239, 0, 0, 1)) {
		t.Fatalf("expected first sub group 239.0.0.1, got %v", pr.MulticastSubGroups[0])
	}
	if !pr.MulticastSubGroups[1].Equal(net.IPv4(239, 0, 0, 2)) {
		t.Fatalf("expected second sub group 239.0.0.2, got %v", pr.MulticastSubGroups[1])
	}
}

func TestReconcile_NoReProvisionWhenUnchanged(t *testing.T) {
	devicePK := [32]byte{1}
	mcastGroupPK := [32]byte{2}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	user := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeMulticast, serviceability.UserStatusActivated)
	user.Subscribers = [][32]uint8{mcastGroupPK}

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:          testConfig(),
			Devices:         []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users:           []serviceability.User{user},
			MulticastGroups: []serviceability.MulticastGroup{{PubKey: mcastGroupPK, MulticastIp: [4]uint8{239, 0, 0, 1}}},
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	// Capture the service pointer after first provision.
	firstService := n.MulticastService
	if firstService == nil {
		t.Fatal("expected multicast service to be provisioned")
	}

	// Reconcile again with same state — service should not be re-created.
	n.reconcile(context.Background())

	if n.MulticastService != firstService {
		t.Fatal("service was re-provisioned despite no state change")
	}
}

func TestMapUserType(t *testing.T) {
	tests := []struct {
		sdk    serviceability.UserUserType
		daemon api.UserType
	}{
		{serviceability.UserTypeIBRL, api.UserTypeIBRL},
		{serviceability.UserTypeIBRLWithAllocatedIP, api.UserTypeIBRLWithAllocatedIP},
		{serviceability.UserTypeEdgeFiltering, api.UserTypeEdgeFiltering},
		{serviceability.UserTypeMulticast, api.UserTypeMulticast},
		{serviceability.UserUserType(99), api.UserTypeUnknown},
	}

	for _, tt := range tests {
		got := mapUserType(tt.sdk)
		if got != tt.daemon {
			t.Errorf("mapUserType(%d) = %v, want %v", tt.sdk, got, tt.daemon)
		}
	}
}

func TestParseOnchainNet(t *testing.T) {
	tests := []struct {
		raw    [5]uint8
		expect string
		valid  bool
	}{
		{[5]uint8{10, 1, 0, 0, 31}, "10.1.0.0/31", true},
		{[5]uint8{10, 0, 0, 0, 24}, "10.0.0.0/24", true},
		{[5]uint8{192, 168, 1, 0, 32}, "192.168.1.0/32", true},
		{[5]uint8{0, 0, 0, 0, 33}, "", false}, // invalid prefix
	}

	for _, tt := range tests {
		got := parseOnchainNet(tt.raw)
		if tt.valid {
			if got == nil {
				t.Fatalf("expected non-nil for %v", tt.raw)
			}
			if got.String() != tt.expect {
				t.Fatalf("expected %s, got %s", tt.expect, got.String())
			}
		} else {
			if got != nil {
				t.Fatalf("expected nil for %v, got %s", tt.raw, got.String())
			}
		}
	}
}

func TestReconcile_BothUnicastAndMulticast(t *testing.T) {
	devicePK := [32]byte{1}
	mcastGroupPK := [32]byte{2}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	ibrlUser := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)
	mcastUser := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeMulticast, serviceability.UserStatusActivated)
	mcastUser.Subscribers = [][32]uint8{mcastGroupPK}

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:          testConfig(),
			Devices:         []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users:           []serviceability.User{ibrlUser, mcastUser},
			MulticastGroups: []serviceability.MulticastGroup{{PubKey: mcastGroupPK, MulticastIp: [4]uint8{239, 0, 0, 1}}},
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.UnicastService == nil {
		t.Fatal("expected unicast service to be provisioned")
	}
	if n.MulticastService == nil {
		t.Fatal("expected multicast service to be provisioned")
	}
}

func TestReconcile_PrefixesCollectedFromAllDevices(t *testing.T) {
	devicePK1 := [32]byte{1}
	devicePK2 := [32]byte{2}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config: testConfig(),
			Devices: []serviceability.Device{
				testDevice(devicePK1, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}}),
				testDevice(devicePK2, [4]uint8{5, 6, 7, 9}, [][5]uint8{{10, 1, 0, 0, 24}}),
			},
			Users: []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK1, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.UnicastService == nil {
		t.Fatal("expected unicast service to be provisioned")
	}
	pr := n.UnicastService.ProvisionRequest()
	if len(pr.DoubleZeroPrefixes) != 2 {
		t.Fatalf("expected 2 prefixes, got %d", len(pr.DoubleZeroPrefixes))
	}
}

func TestReconcile_ResolveTunnelSrc(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()
	resolvedSrc := net.IPv4(10, 0, 0, 99).To4()

	// Use IBRLWithAllocatedIP — only this type and Multicast resolve tunnel
	// src from the routing table; regular IBRL uses clientIP directly.
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRLWithAllocatedIP, serviceability.UserStatusActivated)},
		},
	}

	nl := &mockNetlink{
		routes: []*routing.Route{
			{Src: resolvedSrc, Dst: &net.IPNet{IP: net.IPv4(5, 6, 7, 8), Mask: net.CIDRMask(32, 32)}},
		},
	}
	n := newTestNLMWithNetlink(nl, fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.UnicastService == nil {
		t.Fatal("expected unicast service to be provisioned")
	}
	pr := n.UnicastService.ProvisionRequest()
	if !pr.TunnelSrc.Equal(resolvedSrc) {
		t.Fatalf("expected tunnel src %v (resolved), got %v", resolvedSrc, pr.TunnelSrc)
	}
}

func TestReconcile_ResolveTunnelSrcSkippedForRegularIBRL(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()
	resolvedSrc := net.IPv4(10, 0, 0, 99).To4()

	// Regular IBRL should NOT resolve tunnel src — it uses clientIP directly.
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	nl := &mockNetlink{
		routes: []*routing.Route{
			{Src: resolvedSrc, Dst: &net.IPNet{IP: net.IPv4(5, 6, 7, 8), Mask: net.CIDRMask(32, 32)}},
		},
	}
	n := newTestNLMWithNetlink(nl, fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.UnicastService == nil {
		t.Fatal("expected unicast service to be provisioned")
	}
	pr := n.UnicastService.ProvisionRequest()
	if !pr.TunnelSrc.Equal(clientIP) {
		t.Fatalf("expected tunnel src %v (clientIP, no resolution for regular IBRL), got %v", clientIP, pr.TunnelSrc)
	}
}

func TestReconcile_ResolveTunnelSrcFallback(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	// Use IBRLWithAllocatedIP to test the fallback path when resolution fails.
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRLWithAllocatedIP, serviceability.UserStatusActivated)},
		},
	}

	// No routes → ResolveTunnelSrc returns error → falls back to clientIP
	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.UnicastService == nil {
		t.Fatal("expected unicast service to be provisioned")
	}
	pr := n.UnicastService.ProvisionRequest()
	if !pr.TunnelSrc.Equal(clientIP) {
		t.Fatalf("expected tunnel src %v (fallback to clientIP), got %v", clientIP, pr.TunnelSrc)
	}
}

func TestReconcile_DuplicateUnicastUsersOnlyProvisionFirst(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	user1 := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)
	user2 := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeEdgeFiltering, serviceability.UserStatusActivated)

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{user1, user2},
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.UnicastService == nil {
		t.Fatal("expected unicast service to be provisioned")
	}
	pr := n.UnicastService.ProvisionRequest()
	if pr.UserType != api.UserTypeIBRL {
		t.Fatalf("expected UserTypeIBRL (first user), got %v", pr.UserType)
	}
}

func TestReconcile_DuplicateMulticastUsersOnlyProvisionFirst(t *testing.T) {
	devicePK := [32]byte{1}
	mcastGroupPK1 := [32]byte{2}
	mcastGroupPK2 := [32]byte{3}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	user1 := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeMulticast, serviceability.UserStatusActivated)
	user1.Subscribers = [][32]uint8{mcastGroupPK1}

	user2 := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeMulticast, serviceability.UserStatusActivated)
	user2.Subscribers = [][32]uint8{mcastGroupPK2}

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users:   []serviceability.User{user1, user2},
			MulticastGroups: []serviceability.MulticastGroup{
				{PubKey: mcastGroupPK1, MulticastIp: [4]uint8{239, 0, 0, 1}},
				{PubKey: mcastGroupPK2, MulticastIp: [4]uint8{239, 0, 0, 2}},
			},
		},
	}

	n := newTestNLM(fetcher, WithClientIP(clientIP), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.MulticastService == nil {
		t.Fatal("expected multicast service to be provisioned")
	}
	pr := n.MulticastService.ProvisionRequest()
	if len(pr.MulticastSubGroups) != 1 {
		t.Fatalf("expected 1 sub group from first user, got %d", len(pr.MulticastSubGroups))
	}
	if !pr.MulticastSubGroups[0].Equal(net.IPv4(239, 0, 0, 1)) {
		t.Fatalf("expected sub group 239.0.0.1, got %v", pr.MulticastSubGroups[0])
	}
}

// --- Start() lifecycle tests ---

func TestStartReconciler_ContextCancellation(t *testing.T) {
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{Config: testConfig()},
	}

	n := newTestNLM(fetcher,
		WithClientIP(net.IPv4(1, 2, 3, 4).To4()),
		WithPollInterval(time.Hour),
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- n.StartReconciler(ctx) }()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartReconciler did not return after context cancellation")
	}
}

func TestStartReconciler_InitialReconcileWhenEnabled(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	n := newTestNLM(fetcher,
		WithClientIP(clientIP),
		WithEnabled(true),
		WithPollInterval(time.Hour),
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- n.StartReconciler(ctx) }()

	// Give the initial reconcile time to execute
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if fetcher.calls < 1 {
		t.Fatal("expected at least 1 fetch call from initial reconcile")
	}
	if n.UnicastService == nil {
		t.Fatal("expected unicast service to be provisioned from initial reconcile")
	}
}

func TestStartReconciler_NoReconcileWhenDisabled(t *testing.T) {
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{Config: testConfig()},
	}

	n := newTestNLM(fetcher,
		WithClientIP(net.IPv4(1, 2, 3, 4).To4()),
		WithPollInterval(time.Hour),
	)
	// Default is disabled (no WithEnabled)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- n.StartReconciler(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if fetcher.calls != 0 {
		t.Fatalf("expected 0 fetch calls when disabled, got %d", fetcher.calls)
	}
}

func TestStartReconciler_EnableViaChannel(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	n := newTestNLM(fetcher,
		WithClientIP(clientIP),
		WithPollInterval(time.Hour),
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- n.StartReconciler(ctx) }()

	// Send enable signal
	n.SetEnabled(true)
	time.Sleep(100 * time.Millisecond)

	if !n.Enabled() {
		t.Fatal("expected enabled=true after SetEnabled(true)")
	}
	if fetcher.Calls() < 1 {
		t.Fatal("expected at least 1 fetch call after enable")
	}
	if !n.HasUnicastService() {
		t.Fatal("expected unicast service to be provisioned after enable")
	}

	cancel()
	<-done
}

func TestStartReconciler_DisableViaChannel_TearsDown(t *testing.T) {
	devicePK := [32]byte{1}
	mcastGroupPK := [32]byte{2}

	mcastUser := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeMulticast, serviceability.UserStatusActivated)
	mcastUser.Subscribers = [][32]uint8{mcastGroupPK}

	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users: []serviceability.User{
				testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated),
				mcastUser,
			},
			MulticastGroups: []serviceability.MulticastGroup{{PubKey: mcastGroupPK, MulticastIp: [4]uint8{239, 0, 0, 1}}},
		},
	}

	n := newTestNLM(fetcher,
		WithClientIP(net.IPv4(1, 2, 3, 4).To4()),
		WithEnabled(true),
		WithPollInterval(time.Hour),
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- n.StartReconciler(ctx) }()

	// Wait for initial reconcile, then disable
	time.Sleep(100 * time.Millisecond)
	n.SetEnabled(false)
	time.Sleep(100 * time.Millisecond)

	if n.Enabled() {
		t.Fatal("expected enabled=false after SetEnabled(false)")
	}
	if n.HasUnicastService() {
		t.Fatal("expected unicast service to be removed after disable")
	}
	if n.HasMulticastService() {
		t.Fatal("expected multicast service to be removed after disable")
	}

	cancel()
	<-done
}

func TestStartReconciler_TickerReconcileWhenEnabled(t *testing.T) {
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{Config: testConfig()},
	}

	n := newTestNLM(fetcher,
		WithClientIP(net.IPv4(1, 2, 3, 4).To4()),
		WithEnabled(true),
		WithPollInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- n.StartReconciler(ctx) }()

	// Wait for initial + a few ticker reconciles
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	// Initial + at least 1 ticker-driven reconcile
	if fetcher.calls < 2 {
		t.Fatalf("expected at least 2 fetch calls (initial + ticker), got %d", fetcher.calls)
	}
}

// --- Teardown tests ---

func TestReconcilerTeardown_RemovesBothServices(t *testing.T) {
	n := newTestNLM(&mockFetcher{data: &serviceability.ProgramData{Config: testConfig()}})
	provisionUnicast(t, n)
	provisionMulticast(t, n)

	n.reconcilerTeardown()

	if n.UnicastService != nil {
		t.Fatal("expected unicast service to be removed")
	}
	if n.MulticastService != nil {
		t.Fatal("expected multicast service to be removed")
	}
}

func TestReconcilerTeardown_SkipsAbsentServices(t *testing.T) {
	n := newTestNLM(&mockFetcher{data: &serviceability.ProgramData{Config: testConfig()}})

	// Should not panic or error with no services
	n.reconcilerTeardown()

	if n.UnicastService != nil {
		t.Fatal("expected no unicast service")
	}
	if n.MulticastService != nil {
		t.Fatal("expected no multicast service")
	}
}

func TestReconcilerTeardown_OnlyUnicast(t *testing.T) {
	n := newTestNLM(&mockFetcher{data: &serviceability.ProgramData{Config: testConfig()}})
	provisionUnicast(t, n)

	n.reconcilerTeardown()

	if n.UnicastService != nil {
		t.Fatal("expected unicast service to be removed")
	}
}

func TestReconcilerTeardown_OnlyMulticast(t *testing.T) {
	n := newTestNLM(&mockFetcher{data: &serviceability.ProgramData{Config: testConfig()}})
	provisionMulticast(t, n)

	n.reconcilerTeardown()

	if n.MulticastService != nil {
		t.Fatal("expected multicast service to be removed")
	}
}

// --- Error handling tests ---

func TestReconcile_FetchError(t *testing.T) {
	fetcher := &mockFetcher{
		err: fmt.Errorf("rpc error"),
	}

	n := newTestNLM(fetcher, WithClientIP(net.IPv4(1, 2, 3, 4).To4()), WithPollInterval(time.Second))
	n.reconcile(context.Background())

	if n.UnicastService != nil {
		t.Fatal("expected no provision on fetch error")
	}
}

// --- HTTP handler tests ---

func newTestNLMForHTTP(stateDir string) *NetlinkManager {
	fetcher := &mockFetcher{data: &serviceability.ProgramData{Config: testConfig()}}
	return newTestNLM(fetcher,
		WithClientIP(net.IPv4(1, 2, 3, 4).To4()),
		WithPollInterval(time.Hour),
		WithStateDir(stateDir),
	)
}

func TestServeEnable(t *testing.T) {
	dir := t.TempDir()
	n := newTestNLMForHTTP(dir)

	req := httptest.NewRequest(http.MethodPost, "/enable", nil)
	w := httptest.NewRecorder()
	n.ServeEnable(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("expected status=ok, got %s", resp["status"])
	}

	// State file should be written
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"reconciler_enabled":true}` {
		t.Fatalf("unexpected state file: %s", data)
	}

	// Drain the enable signal from the channel
	select {
	case enabled := <-n.enableCh:
		if !enabled {
			t.Fatal("expected enable signal to be true")
		}
	default:
		t.Fatal("expected enable signal on channel")
	}
}

func TestServeDisable(t *testing.T) {
	dir := t.TempDir()
	n := newTestNLMForHTTP(dir)
	n.enabled.Store(true)

	req := httptest.NewRequest(http.MethodPost, "/disable", nil)
	w := httptest.NewRecorder()
	n.ServeDisable(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("expected status=ok, got %s", resp["status"])
	}

	// State file should be written
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"reconciler_enabled":false}` {
		t.Fatalf("unexpected state file: %s", data)
	}

	// Drain the disable signal from the channel
	select {
	case enabled := <-n.enableCh:
		if enabled {
			t.Fatal("expected disable signal to be false")
		}
	default:
		t.Fatal("expected disable signal on channel")
	}
}

func TestServeEnable_AlreadyEnabled(t *testing.T) {
	dir := t.TempDir()
	n := newTestNLMForHTTP(dir)
	n.enabled.Store(true)

	// Write existing enabled state
	if err := WriteState(dir, true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/enable", nil)
	w := httptest.NewRecorder()
	n.ServeEnable(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even when already enabled, got %d", w.Code)
	}

	// State file should still say enabled
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"reconciler_enabled":true}` {
		t.Fatalf("unexpected state file: %s", data)
	}

	// No signal should be sent since the reconciler is already enabled.
	select {
	case <-n.enableCh:
		t.Fatal("expected no signal on channel when already enabled")
	default:
	}
}

func TestServeDisable_AlreadyDisabled(t *testing.T) {
	dir := t.TempDir()
	n := newTestNLMForHTTP(dir)
	// Default is disabled

	// Write existing disabled state
	if err := WriteState(dir, false); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/disable", nil)
	w := httptest.NewRecorder()
	n.ServeDisable(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even when already disabled, got %d", w.Code)
	}

	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"reconciler_enabled":false}` {
		t.Fatalf("unexpected state file: %s", data)
	}
}

func TestServeEnable_StateDirNotWritable(t *testing.T) {
	// Use a path that can't be created
	n := newTestNLMForHTTP("/dev/null/impossible")

	req := httptest.NewRequest(http.MethodPost, "/enable", nil)
	w := httptest.NewRecorder()
	n.ServeEnable(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when state dir is not writable, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "error" {
		t.Fatalf("expected status=error, got %s", resp["status"])
	}
}

func TestServeV2Status_Enabled_WithServices(t *testing.T) {
	dir := t.TempDir()
	n := newTestNLMForHTTP(dir)
	n.enabled.Store(true)
	provisionUnicast(t, n)

	req := httptest.NewRequest(http.MethodGet, "/v2/status", nil)
	w := httptest.NewRecorder()
	n.ServeV2Status(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp V2StatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.ReconcilerEnabled {
		t.Fatal("expected reconciler_enabled=true")
	}
	if len(resp.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(resp.Services))
	}
}

func TestServeV2Status_Disabled_NoServices(t *testing.T) {
	dir := t.TempDir()
	n := newTestNLMForHTTP(dir)
	// Default is disabled, no services

	req := httptest.NewRequest(http.MethodGet, "/v2/status", nil)
	w := httptest.NewRecorder()
	n.ServeV2Status(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp V2StatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ReconcilerEnabled {
		t.Fatal("expected reconciler_enabled=false")
	}
	if len(resp.Services) != 0 {
		t.Fatalf("expected 0 services, got %d", len(resp.Services))
	}
}

// --- Startup migration integration tests ---

func TestStartup_UpgradeFromOldDaemon_WasConnected(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, oldStateFileName)
	// Old doublezerod.json stored []*api.ProvisionRequest as a JSON array.
	// A non-empty array means the client had active tunnels.
	oldContent := `[{"user_type":"IBRL","tunnel_src":"1.2.3.4","tunnel_dst":"5.6.7.8","tunnel_net":"10.0.0.0/30","doublezero_ip":"10.0.0.1","doublezero_prefixes":["10.0.0.0/24"]}]`
	if err := os.WriteFile(oldPath, []byte(oldContent), 0644); err != nil {
		t.Fatal(err)
	}

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("migration from old file should yield enabled=true")
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("old doublezerod.json should be deleted after migration")
	}

	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"reconciler_enabled":true}` {
		t.Fatalf("unexpected state file after migration: %s", data)
	}

	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	n := newTestNLM(fetcher,
		WithClientIP(clientIP),
		WithEnabled(enabled),
		WithPollInterval(time.Hour),
		WithStateDir(dir),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- n.StartReconciler(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if fetcher.calls < 1 {
		t.Fatal("expected reconciler to fetch after migration-enabled startup")
	}
	if n.UnicastService == nil {
		t.Fatal("expected unicast service to be provisioned after migration-enabled startup")
	}
	pr := n.UnicastService.ProvisionRequest()
	if pr.UserType != api.UserTypeIBRL {
		t.Fatalf("expected UserTypeIBRL, got %v", pr.UserType)
	}
}

func TestStartup_DaemonRestart_WasEnabled(t *testing.T) {
	dir := t.TempDir()
	if err := WriteState(dir, true); err != nil {
		t.Fatal(err)
	}

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("expected enabled=true from existing state file")
	}

	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	n := newTestNLM(fetcher,
		WithClientIP(clientIP),
		WithEnabled(enabled),
		WithPollInterval(time.Hour),
		WithStateDir(dir),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- n.StartReconciler(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if n.UnicastService == nil {
		t.Fatal("expected unicast service to be provisioned after restart with enabled state")
	}
}

func TestStartup_FreshInstall_DoesNotProvision(t *testing.T) {
	dir := t.TempDir()

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("expected enabled=false for fresh install")
	}

	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	n := newTestNLM(fetcher,
		WithClientIP(clientIP),
		WithEnabled(enabled),
		WithPollInterval(time.Hour),
		WithStateDir(dir),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- n.StartReconciler(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if fetcher.calls != 0 {
		t.Fatalf("expected 0 fetch calls for fresh install (disabled), got %d", fetcher.calls)
	}
	if n.UnicastService != nil {
		t.Fatal("expected no provision for fresh install (disabled)")
	}
}

func TestStartup_DaemonRestart_WasDisabled(t *testing.T) {
	dir := t.TempDir()
	if err := WriteState(dir, false); err != nil {
		t.Fatal(err)
	}

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("expected enabled=false")
	}

	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	n := newTestNLM(fetcher,
		WithClientIP(clientIP),
		WithEnabled(enabled),
		WithPollInterval(time.Hour),
		WithStateDir(dir),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- n.StartReconciler(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if fetcher.calls != 0 {
		t.Fatalf("expected 0 fetch calls when restarted disabled, got %d", fetcher.calls)
	}
	if n.UnicastService != nil {
		t.Fatal("expected no provision when restarted disabled")
	}
}

// --- helpers to pre-provision services ---

func provisionUnicast(t *testing.T, n *NetlinkManager) {
	t.Helper()
	pr := api.ProvisionRequest{
		UserType:           api.UserTypeIBRL,
		TunnelSrc:          net.IPv4(1, 1, 1, 1),
		TunnelDst:          net.IPv4(2, 2, 2, 2),
		TunnelNet:          &net.IPNet{IP: net.IPv4(169, 254, 0, 0), Mask: net.CIDRMask(31, 32)},
		DoubleZeroIP:       net.IPv4(10, 0, 0, 1),
		DoubleZeroPrefixes: []*net.IPNet{{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(24, 32)}},
	}
	if err := n.Provision(pr); err != nil {
		t.Fatalf("failed to pre-provision unicast: %v", err)
	}
}

func provisionMulticast(t *testing.T, n *NetlinkManager) {
	t.Helper()
	pr := api.ProvisionRequest{
		UserType:           api.UserTypeMulticast,
		TunnelSrc:          net.IPv4(1, 1, 1, 1),
		TunnelDst:          net.IPv4(2, 2, 2, 2),
		TunnelNet:          &net.IPNet{IP: net.IPv4(169, 254, 0, 0), Mask: net.CIDRMask(31, 32)},
		DoubleZeroIP:       net.IPv4(10, 0, 0, 1),
		DoubleZeroPrefixes: []*net.IPNet{{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(24, 32)}},
		MulticastSubGroups: []net.IP{net.IPv4(239, 0, 0, 1)},
	}
	if err := n.Provision(pr); err != nil {
		t.Fatalf("failed to pre-provision multicast: %v", err)
	}
}
