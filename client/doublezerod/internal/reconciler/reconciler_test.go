package reconciler

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type mockManager struct {
	provisions       []api.ProvisionRequest
	removes          []api.UserType
	hasUnicast       bool
	hasMulticast     bool
	provisionErr     error
	removeErr        error
	resolvedSrc      net.IP
	resolveSrcErr    error
}

func (m *mockManager) Provision(pr api.ProvisionRequest) error {
	m.provisions = append(m.provisions, pr)
	return m.provisionErr
}

func (m *mockManager) Remove(ut api.UserType) error {
	m.removes = append(m.removes, ut)
	return m.removeErr
}

func (m *mockManager) HasUnicastService() bool   { return m.hasUnicast }
func (m *mockManager) HasMulticastService() bool { return m.hasMulticast }

func (m *mockManager) ResolveTunnelSrc(dst net.IP) (net.IP, error) {
	if m.resolveSrcErr != nil {
		return nil, m.resolveSrcErr
	}
	if m.resolvedSrc != nil {
		return m.resolvedSrc, nil
	}
	return nil, fmt.Errorf("no route found")
}

type mockFetcher struct {
	data *serviceability.ProgramData
	err  error
}

func (m *mockFetcher) GetProgramData(_ context.Context) (*serviceability.ProgramData, error) {
	return m.data, m.err
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
