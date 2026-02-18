package reconciler

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
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type mockManager struct {
	mu            sync.Mutex
	provisions    []api.ProvisionRequest
	removes       []api.UserType
	hasUnicast    bool
	hasMulticast  bool
	provisionErr  error
	removeErr     error
	resolvedSrc   net.IP
	resolveSrcErr error
	statusResp    []*api.StatusResponse
	statusErr     error
}

func (m *mockManager) Provision(pr api.ProvisionRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provisions = append(m.provisions, pr)
	return m.provisionErr
}

func (m *mockManager) Remove(ut api.UserType) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removes = append(m.removes, ut)
	return m.removeErr
}

func (m *mockManager) HasUnicastService() bool   { return m.hasUnicast }
func (m *mockManager) HasMulticastService() bool { return m.hasMulticast }
func (m *mockManager) Status() ([]*api.StatusResponse, error) {
	return m.statusResp, m.statusErr
}

func (m *mockManager) ResolveTunnelSrc(dst net.IP) (net.IP, error) {
	if m.resolveSrcErr != nil {
		return nil, m.resolveSrcErr
	}
	if m.resolvedSrc != nil {
		return m.resolvedSrc, nil
	}
	return nil, fmt.Errorf("no route found")
}

func (m *mockManager) Provisions() []api.ProvisionRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]api.ProvisionRequest(nil), m.provisions...)
}

func (m *mockManager) Removes() []api.UserType {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]api.UserType(nil), m.removes...)
}

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

func TestReconcile_ProvisionUnicast(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.provisions) != 1 {
		t.Fatalf("expected 1 provision call, got %d", len(mgr.provisions))
	}
	if mgr.provisions[0].UserType != api.UserTypeIBRL {
		t.Fatalf("expected UserTypeIBRL, got %v", mgr.provisions[0].UserType)
	}
	if !mgr.provisions[0].TunnelDst.Equal(net.IPv4(5, 6, 7, 8)) {
		t.Fatalf("expected tunnel dst 5.6.7.8, got %v", mgr.provisions[0].TunnelDst)
	}
	if mgr.provisions[0].BgpLocalAsn != 65000 {
		t.Fatalf("expected bgp local asn 65000, got %d", mgr.provisions[0].BgpLocalAsn)
	}
}

func TestReconcile_ProvisionUnicast_WithTunnelEndpoint(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	user := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)
	user.TunnelEndpoint = [4]uint8{10, 0, 0, 99}

	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{user},
		},
	}

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.provisions) != 1 {
		t.Fatalf("expected 1 provision call, got %d", len(mgr.provisions))
	}
	if !mgr.provisions[0].TunnelDst.Equal(net.IPv4(10, 0, 0, 99)) {
		t.Fatalf("expected tunnel dst 10.0.0.99 (from TunnelEndpoint), got %v", mgr.provisions[0].TunnelDst)
	}
}

func TestReconcile_RemoveUnicast(t *testing.T) {
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	mgr := &mockManager{hasUnicast: true}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config: testConfig(),
			Users:  []serviceability.User{}, // no matching users
		},
	}

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.removes) != 1 {
		t.Fatalf("expected 1 remove call, got %d", len(mgr.removes))
	}
	if mgr.removes[0] != api.UserTypeIBRL {
		t.Fatalf("expected remove UserTypeIBRL, got %v", mgr.removes[0])
	}
}

func TestReconcile_NoopWhenAlreadyProvisioned(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	mgr := &mockManager{hasUnicast: true}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.provisions) != 0 {
		t.Fatalf("expected 0 provision calls, got %d", len(mgr.provisions))
	}
	if len(mgr.removes) != 0 {
		t.Fatalf("expected 0 remove calls, got %d", len(mgr.removes))
	}
}

func TestReconcile_NoopWhenNoMatchingUsers(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users:   []serviceability.User{testUser([4]uint8{9, 9, 9, 9}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.provisions) != 0 {
		t.Fatalf("expected 0 provision calls, got %d", len(mgr.provisions))
	}
	if len(mgr.removes) != 0 {
		t.Fatalf("expected 0 remove calls, got %d", len(mgr.removes))
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
		mgr := &mockManager{}
		fetcher := &mockFetcher{
			data: &serviceability.ProgramData{
				Config:  testConfig(),
				Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
				Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, status)},
			},
		}

		r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
		r.reconcile(context.Background())

		if len(mgr.provisions) != 0 {
			t.Fatalf("status %d: expected 0 provision calls, got %d", status, len(mgr.provisions))
		}
	}
}

func TestReconcile_ProvisionMulticast(t *testing.T) {
	devicePK := [32]byte{1}
	mcastGroupPK := [32]byte{2}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	user := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeMulticast, serviceability.UserStatusActivated)
	user.Subscribers = [][32]uint8{mcastGroupPK}

	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:          testConfig(),
			Devices:         []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users:           []serviceability.User{user},
			MulticastGroups: []serviceability.MulticastGroup{{PubKey: mcastGroupPK, MulticastIp: [4]uint8{239, 0, 0, 1}}},
		},
	}

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.provisions) != 1 {
		t.Fatalf("expected 1 provision call, got %d", len(mgr.provisions))
	}
	if mgr.provisions[0].UserType != api.UserTypeMulticast {
		t.Fatalf("expected UserTypeMulticast, got %v", mgr.provisions[0].UserType)
	}
	if len(mgr.provisions[0].MulticastSubGroups) != 1 {
		t.Fatalf("expected 1 sub group, got %d", len(mgr.provisions[0].MulticastSubGroups))
	}
	if !mgr.provisions[0].MulticastSubGroups[0].Equal(net.IPv4(239, 0, 0, 1)) {
		t.Fatalf("expected sub group 239.0.0.1, got %v", mgr.provisions[0].MulticastSubGroups[0])
	}
}

func TestReconcile_RemoveMulticast(t *testing.T) {
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	mgr := &mockManager{hasMulticast: true}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config: testConfig(),
			Users:  []serviceability.User{},
		},
	}

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.removes) != 1 {
		t.Fatalf("expected 1 remove call, got %d", len(mgr.removes))
	}
	if mgr.removes[0] != api.UserTypeMulticast {
		t.Fatalf("expected remove UserTypeMulticast, got %v", mgr.removes[0])
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
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	ibrlUser := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)
	mcastUser := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeMulticast, serviceability.UserStatusActivated)

	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users:   []serviceability.User{ibrlUser, mcastUser},
		},
	}

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.provisions) != 2 {
		t.Fatalf("expected 2 provision calls, got %d", len(mgr.provisions))
	}
}

func TestReconcile_PrefixesCollectedFromAllDevices(t *testing.T) {
	devicePK1 := [32]byte{1}
	devicePK2 := [32]byte{2}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	mgr := &mockManager{}
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

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.provisions) != 1 {
		t.Fatalf("expected 1 provision call, got %d", len(mgr.provisions))
	}
	if len(mgr.provisions[0].DoubleZeroPrefixes) != 2 {
		t.Fatalf("expected 2 prefixes, got %d", len(mgr.provisions[0].DoubleZeroPrefixes))
	}
}

func TestReconcile_ResolveTunnelSrc(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()
	resolvedSrc := net.IPv4(10, 0, 0, 99).To4()

	mgr := &mockManager{resolvedSrc: resolvedSrc}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.provisions) != 1 {
		t.Fatalf("expected 1 provision call, got %d", len(mgr.provisions))
	}
	if !mgr.provisions[0].TunnelSrc.Equal(resolvedSrc) {
		t.Fatalf("expected tunnel src %v (resolved), got %v", resolvedSrc, mgr.provisions[0].TunnelSrc)
	}
}

func TestReconcile_ResolveTunnelSrcFallback(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	// resolvedSrc is nil, resolveSrcErr is nil -> falls back to clientIP
	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.provisions) != 1 {
		t.Fatalf("expected 1 provision call, got %d", len(mgr.provisions))
	}
	if !mgr.provisions[0].TunnelSrc.Equal(clientIP) {
		t.Fatalf("expected tunnel src %v (fallback to clientIP), got %v", clientIP, mgr.provisions[0].TunnelSrc)
	}
}

func TestReconcile_DuplicateUnicastUsersOnlyProvisionFirst(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	user1 := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)
	user2 := testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeEdgeFiltering, serviceability.UserStatusActivated)

	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{user1, user2},
		},
	}

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.provisions) != 1 {
		t.Fatalf("expected 1 provision call (first unicast user only), got %d", len(mgr.provisions))
	}
	if mgr.provisions[0].UserType != api.UserTypeIBRL {
		t.Fatalf("expected UserTypeIBRL (first user), got %v", mgr.provisions[0].UserType)
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

	mgr := &mockManager{}
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

	r := &Reconciler{clientIP: clientIP, manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.provisions) != 1 {
		t.Fatalf("expected 1 provision call (first multicast user only), got %d", len(mgr.provisions))
	}
	if len(mgr.provisions[0].MulticastSubGroups) != 1 {
		t.Fatalf("expected 1 sub group from first user, got %d", len(mgr.provisions[0].MulticastSubGroups))
	}
	if !mgr.provisions[0].MulticastSubGroups[0].Equal(net.IPv4(239, 0, 0, 1)) {
		t.Fatalf("expected sub group 239.0.0.1, got %v", mgr.provisions[0].MulticastSubGroups[0])
	}
}

// --- Start() lifecycle tests ---

func TestStart_ContextCancellation(t *testing.T) {
	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{Config: testConfig()},
	}

	r := NewReconciler(net.IPv4(1, 2, 3, 4).To4(), mgr, fetcher,
		WithPollInterval(time.Hour), // long interval — we only care about shutdown
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after context cancellation")
	}
}

func TestStart_InitialReconcileWhenEnabled(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	r := NewReconciler(clientIP, mgr, fetcher,
		WithEnabled(true),
		WithPollInterval(time.Hour), // long interval — we only test initial reconcile
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	// Give the initial reconcile time to execute
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if fetcher.calls < 1 {
		t.Fatal("expected at least 1 fetch call from initial reconcile")
	}
	if len(mgr.provisions) != 1 {
		t.Fatalf("expected 1 provision from initial reconcile, got %d", len(mgr.provisions))
	}
}

func TestStart_NoReconcileWhenDisabled(t *testing.T) {
	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{Config: testConfig()},
	}

	r := NewReconciler(net.IPv4(1, 2, 3, 4).To4(), mgr, fetcher,
		WithPollInterval(time.Hour),
	)
	// Default is disabled (no WithEnabled)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if fetcher.calls != 0 {
		t.Fatalf("expected 0 fetch calls when disabled, got %d", fetcher.calls)
	}
}

func TestStart_EnableViaChannel(t *testing.T) {
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()

	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	r := NewReconciler(clientIP, mgr, fetcher,
		WithPollInterval(time.Hour),
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	// Send enable signal
	r.SetEnabled(true)
	time.Sleep(100 * time.Millisecond)

	if !r.Enabled() {
		t.Fatal("expected enabled=true after SetEnabled(true)")
	}
	if fetcher.Calls() < 1 {
		t.Fatal("expected at least 1 fetch call after enable")
	}
	if len(mgr.Provisions()) < 1 {
		t.Fatal("expected at least 1 provision after enable")
	}

	cancel()
	<-done
}

func TestStart_DisableViaChannel_TearsDown(t *testing.T) {
	devicePK := [32]byte{1}
	mgr := &mockManager{hasUnicast: true, hasMulticast: true}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, nil)},
			Users: []serviceability.User{
				testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated),
				testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeMulticast, serviceability.UserStatusActivated),
			},
		},
	}

	r := NewReconciler(net.IPv4(1, 2, 3, 4).To4(), mgr, fetcher,
		WithEnabled(true),
		WithPollInterval(time.Hour),
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	// Wait for initial reconcile, then disable
	time.Sleep(100 * time.Millisecond)
	r.SetEnabled(false)
	time.Sleep(100 * time.Millisecond)

	if r.Enabled() {
		t.Fatal("expected enabled=false after SetEnabled(false)")
	}
	if removes := mgr.Removes(); len(removes) != 2 {
		t.Fatalf("expected 2 remove calls (unicast + multicast teardown), got %d", len(removes))
	}

	cancel()
	<-done
}

func TestStart_TickerReconcileWhenEnabled(t *testing.T) {
	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{Config: testConfig()},
	}

	r := NewReconciler(net.IPv4(1, 2, 3, 4).To4(), mgr, fetcher,
		WithEnabled(true),
		WithPollInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

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

func TestTeardown_RemovesBothServices(t *testing.T) {
	mgr := &mockManager{hasUnicast: true, hasMulticast: true}
	r := &Reconciler{manager: mgr}

	r.teardown()

	if len(mgr.removes) != 2 {
		t.Fatalf("expected 2 remove calls, got %d", len(mgr.removes))
	}
	if mgr.removes[0] != api.UserTypeIBRL {
		t.Fatalf("expected first remove UserTypeIBRL, got %v", mgr.removes[0])
	}
	if mgr.removes[1] != api.UserTypeMulticast {
		t.Fatalf("expected second remove UserTypeMulticast, got %v", mgr.removes[1])
	}
}

func TestTeardown_SkipsAbsentServices(t *testing.T) {
	mgr := &mockManager{hasUnicast: false, hasMulticast: false}
	r := &Reconciler{manager: mgr}

	r.teardown()

	if len(mgr.removes) != 0 {
		t.Fatalf("expected 0 remove calls when no services, got %d", len(mgr.removes))
	}
}

func TestTeardown_OnlyUnicast(t *testing.T) {
	mgr := &mockManager{hasUnicast: true, hasMulticast: false}
	r := &Reconciler{manager: mgr}

	r.teardown()

	if len(mgr.removes) != 1 {
		t.Fatalf("expected 1 remove call, got %d", len(mgr.removes))
	}
	if mgr.removes[0] != api.UserTypeIBRL {
		t.Fatalf("expected remove UserTypeIBRL, got %v", mgr.removes[0])
	}
}

func TestTeardown_OnlyMulticast(t *testing.T) {
	mgr := &mockManager{hasUnicast: false, hasMulticast: true}
	r := &Reconciler{manager: mgr}

	r.teardown()

	if len(mgr.removes) != 1 {
		t.Fatalf("expected 1 remove call, got %d", len(mgr.removes))
	}
	if mgr.removes[0] != api.UserTypeMulticast {
		t.Fatalf("expected remove UserTypeMulticast, got %v", mgr.removes[0])
	}
}

// --- Error handling tests ---

func TestReconcile_FetchError(t *testing.T) {
	mgr := &mockManager{}
	fetcher := &mockFetcher{
		err: fmt.Errorf("rpc error"),
	}

	r := &Reconciler{clientIP: net.IPv4(1, 2, 3, 4).To4(), manager: mgr, fetcher: fetcher, pollInterval: time.Second}
	r.reconcile(context.Background())

	if len(mgr.provisions) != 0 {
		t.Fatalf("expected 0 provisions on fetch error, got %d", len(mgr.provisions))
	}
	if len(mgr.removes) != 0 {
		t.Fatalf("expected 0 removes on fetch error, got %d", len(mgr.removes))
	}
}

// --- HTTP handler tests ---

func newTestReconciler(mgr *mockManager, stateDir string) *Reconciler {
	fetcher := &mockFetcher{data: &serviceability.ProgramData{Config: testConfig()}}
	return NewReconciler(net.IPv4(1, 2, 3, 4).To4(), mgr, fetcher,
		WithPollInterval(time.Hour),
		WithStateDir(stateDir),
	)
}

func TestServeEnable(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{}
	r := newTestReconciler(mgr, dir)

	req := httptest.NewRequest(http.MethodPost, "/enable", nil)
	w := httptest.NewRecorder()
	r.ServeEnable(w, req)

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
	case enabled := <-r.enableCh:
		if !enabled {
			t.Fatal("expected enable signal to be true")
		}
	default:
		t.Fatal("expected enable signal on channel")
	}
}

func TestServeDisable(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{}
	r := newTestReconciler(mgr, dir)
	r.enabled.Store(true)

	req := httptest.NewRequest(http.MethodPost, "/disable", nil)
	w := httptest.NewRecorder()
	r.ServeDisable(w, req)

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
	case enabled := <-r.enableCh:
		if enabled {
			t.Fatal("expected disable signal to be false")
		}
	default:
		t.Fatal("expected disable signal on channel")
	}
}

func TestServeEnable_AlreadyEnabled(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{}
	r := newTestReconciler(mgr, dir)
	r.enabled.Store(true)

	// Write existing enabled state
	if err := WriteState(dir, true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/enable", nil)
	w := httptest.NewRecorder()
	r.ServeEnable(w, req)

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

	// Channel should still receive the signal (idempotent)
	select {
	case enabled := <-r.enableCh:
		if !enabled {
			t.Fatal("expected enable signal")
		}
	default:
		t.Fatal("expected enable signal on channel")
	}
}

func TestServeDisable_AlreadyDisabled(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{}
	r := newTestReconciler(mgr, dir)
	// Default is disabled

	// Write existing disabled state
	if err := WriteState(dir, false); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/disable", nil)
	w := httptest.NewRecorder()
	r.ServeDisable(w, req)

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
	mgr := &mockManager{}
	r := newTestReconciler(mgr, "/dev/null/impossible")

	req := httptest.NewRequest(http.MethodPost, "/enable", nil)
	w := httptest.NewRecorder()
	r.ServeEnable(w, req)

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
	mgr := &mockManager{
		statusResp: []*api.StatusResponse{
			{TunnelName: "doublezero1", UserType: api.UserTypeIBRL},
		},
	}
	r := newTestReconciler(mgr, dir)
	r.enabled.Store(true)

	req := httptest.NewRequest(http.MethodGet, "/v2/status", nil)
	w := httptest.NewRecorder()
	r.ServeV2Status(w, req)

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
	if resp.Services[0].TunnelName != "doublezero1" {
		t.Fatalf("expected tunnel name doublezero1, got %s", resp.Services[0].TunnelName)
	}
}

func TestServeV2Status_Disabled_NoServices(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{}
	r := newTestReconciler(mgr, dir)
	// Default is disabled, no services

	req := httptest.NewRequest(http.MethodGet, "/v2/status", nil)
	w := httptest.NewRecorder()
	r.ServeV2Status(w, req)

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

func TestServeV2Status_ManagerError(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{statusErr: fmt.Errorf("manager error")}
	r := newTestReconciler(mgr, dir)

	req := httptest.NewRequest(http.MethodGet, "/v2/status", nil)
	w := httptest.NewRecorder()
	r.ServeV2Status(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "error" {
		t.Fatalf("expected status=error, got %s", resp["status"])
	}
}

// --- Startup migration integration tests ---
// These simulate the full daemon startup path: LoadOrMigrateState → NewReconciler(WithEnabled(result)) → Start()

func TestStartup_UpgradeFromOldDaemon_WasConnected(t *testing.T) {
	// Scenario: user was running old daemon with active tunnel (doublezerod.json exists).
	// After upgrade, LoadOrMigrateState migrates to enabled, reconciler starts and provisions.
	dir := t.TempDir()
	oldPath := filepath.Join(dir, oldStateFileName)
	if err := os.WriteFile(oldPath, []byte(`{"tunnel_src":"1.2.3.4"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate daemon startup: load state
	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("migration from old file should yield enabled=true")
	}

	// Old file should be cleaned up
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("old doublezerod.json should be deleted after migration")
	}

	// New state.json should exist
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"reconciler_enabled":true}` {
		t.Fatalf("unexpected state file after migration: %s", data)
	}

	// Simulate daemon creating reconciler with migrated state
	devicePK := [32]byte{1}
	clientIP := net.IPv4(1, 2, 3, 4).To4()
	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	r := NewReconciler(clientIP, mgr, fetcher,
		WithEnabled(enabled),
		WithPollInterval(time.Hour),
		WithStateDir(dir),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if fetcher.calls < 1 {
		t.Fatal("expected reconciler to fetch after migration-enabled startup")
	}
	if len(mgr.provisions) != 1 {
		t.Fatalf("expected 1 provision after migration-enabled startup, got %d", len(mgr.provisions))
	}
	if mgr.provisions[0].UserType != api.UserTypeIBRL {
		t.Fatalf("expected UserTypeIBRL, got %v", mgr.provisions[0].UserType)
	}
}

func TestStartup_DaemonRestart_WasEnabled(t *testing.T) {
	// Scenario: daemon was running with reconciler enabled, then restarted.
	// state.json says enabled → reconciler starts and provisions.
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
	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	r := NewReconciler(clientIP, mgr, fetcher,
		WithEnabled(enabled),
		WithPollInterval(time.Hour),
		WithStateDir(dir),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if len(mgr.provisions) != 1 {
		t.Fatalf("expected 1 provision after restart with enabled state, got %d", len(mgr.provisions))
	}
}

func TestStartup_FreshInstall_DoesNotProvision(t *testing.T) {
	// Scenario: fresh install, no state files. Reconciler starts disabled.
	// Even if an activated user exists onchain, it should NOT provision.
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
	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	r := NewReconciler(clientIP, mgr, fetcher,
		WithEnabled(enabled),
		WithPollInterval(time.Hour),
		WithStateDir(dir),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if fetcher.calls != 0 {
		t.Fatalf("expected 0 fetch calls for fresh install (disabled), got %d", fetcher.calls)
	}
	if len(mgr.provisions) != 0 {
		t.Fatalf("expected 0 provisions for fresh install (disabled), got %d", len(mgr.provisions))
	}
}

func TestStartup_DaemonRestart_WasDisabled(t *testing.T) {
	// Scenario: daemon was running with reconciler disabled, then restarted.
	// state.json says disabled → reconciler should NOT provision.
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
	mgr := &mockManager{}
	fetcher := &mockFetcher{
		data: &serviceability.ProgramData{
			Config:  testConfig(),
			Devices: []serviceability.Device{testDevice(devicePK, [4]uint8{5, 6, 7, 8}, [][5]uint8{{10, 0, 0, 0, 24}})},
			Users:   []serviceability.User{testUser([4]uint8{1, 2, 3, 4}, devicePK, serviceability.UserTypeIBRL, serviceability.UserStatusActivated)},
		},
	}

	r := NewReconciler(clientIP, mgr, fetcher,
		WithEnabled(enabled),
		WithPollInterval(time.Hour),
		WithStateDir(dir),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if fetcher.calls != 0 {
		t.Fatalf("expected 0 fetch calls when restarted disabled, got %d", fetcher.calls)
	}
	if len(mgr.provisions) != 0 {
		t.Fatalf("expected 0 provisions when restarted disabled, got %d", len(mgr.provisions))
	}
}
