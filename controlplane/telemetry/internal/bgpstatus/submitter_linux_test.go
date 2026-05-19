//go:build linux

package bgpstatus

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netutil"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

// staticCollector returns a NamespaceCollector that serves fixed data per namespace.
// Tests that don't care about RTT can pass an established-only map via
// staticEstablishedCollector, which adapts to the BGPPeerState shape.
func staticCollector(
	peers map[string]map[string]BGPPeerState,
	ifaces map[string][]netutil.Interface,
) NamespaceCollector {
	return func(_ context.Context, ns string) (map[string]BGPPeerState, []netutil.Interface, error) {
		return peers[ns], ifaces[ns], nil
	}
}

// staticEstablishedCollector adapts the legacy "set of established IPs" shape
// to the new NamespaceCollector signature, filling RttNs=0 for every entry.
// Use this in tests that only assert Up/Down, not RTT.
func staticEstablishedCollector(
	established map[string]map[string]struct{},
	ifaces map[string][]netutil.Interface,
) NamespaceCollector {
	peers := make(map[string]map[string]BGPPeerState, len(established))
	for ns, ips := range established {
		nsPeers := make(map[string]BGPPeerState, len(ips))
		for ip := range ips {
			nsPeers[ip] = BGPPeerState{Established: true}
		}
		peers[ns] = nsPeers
	}
	return staticCollector(peers, ifaces)
}

// errCollector returns a NamespaceCollector that always fails.
func errCollector(err error) NamespaceCollector {
	return func(_ context.Context, _ string) (map[string]BGPPeerState, []netutil.Interface, error) {
		return nil, nil, err
	}
}

// makeInterface builds a netutil.Interface with one IPv4 /31 address, so that
// FindLocalTunnel can locate it and derive the peer IP.
// cidr must be in host form, e.g. "10.0.2.0/31".
func makeInterface(name, cidr string) netutil.Interface {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	ipnet.IP = ip.To4() // host IP, not network address
	return netutil.Interface{Name: name, Addrs: []net.Addr{ipnet}}
}

// makeActivatedUser returns a User activated on devicePK with the given /31 tunnelNet.
func makeActivatedUser(devicePK solana.PublicKey, tunnelNet [5]byte) serviceability.User {
	u := serviceability.User{}
	copy(u.DevicePubKey[:], devicePK[:])
	userPK := solana.NewWallet().PublicKey()
	copy(u.PubKey[:], userPK[:])
	u.TunnelNet = tunnelNet
	u.Status = serviceability.UserStatusActivated
	return u
}

// makeMulticastUser returns a multicast User activated on devicePK with the given /31 tunnelNet.
func makeMulticastUser(devicePK solana.PublicKey, tunnelNet [5]byte) serviceability.User {
	u := makeActivatedUser(devicePK, tunnelNet)
	u.UserType = serviceability.UserTypeMulticast
	return u
}

// ============================================================
// tick() – multi-namespace collection
// ============================================================

// TestTick_SingleNamespace_SubmitsDown verifies baseline: when the collector
// returns no interfaces (tunnel not found) and status was previously Up,
// tick enqueues a Down submission.
func TestTick_SingleNamespace_SubmitsDown(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	// tunnelNet 10.0.0.0/31
	tunnelNet := [5]byte{10, 0, 0, 0, 31}
	user := makeActivatedUser(devicePK, tunnelNet)
	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()

	exec := &mockExecutor{}
	clk := clockwork.NewFakeClock()
	svc := &mockSvcClient{data: &serviceability.ProgramData{Users: []serviceability.User{user}}}

	// No tunnel interface → BGP is Down.
	col := staticEstablishedCollector(
		map[string]map[string]struct{}{"ns-vrf1": {}},
		map[string][]netutil.Interface{"ns-vrf1": nil},
	)

	s := newTestSubmitter(t, clk, exec, svc, col, devicePK, 0, 6*time.Hour, "ns-vrf1")

	// Seed the user's state as Up so a Down transition is warranted.
	s.mu.Lock()
	s.userState[userPK] = &userState{lastOnchainStatus: serviceability.BGPStatusUp}
	s.mu.Unlock()

	ctx := context.Background()
	s.tick(ctx)

	s.mu.Lock()
	enqueued := len(s.taskCh)
	s.mu.Unlock()

	if enqueued != 1 {
		t.Fatalf("expected 1 task enqueued, got %d", enqueued)
	}
	task := <-s.taskCh
	if task.status != serviceability.BGPStatusDown {
		t.Errorf("expected Down task, got %v", task.status)
	}
}

// TestTick_MultiNamespace_UserInSecondVrf verifies that a user whose tunnel
// lives in ns-vrf2 is found when the collector serves it there.
func TestTick_MultiNamespace_UserInSecondVrf(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	tunnelNet := [5]byte{10, 0, 2, 0, 31} // 10.0.2.0/31

	user := makeActivatedUser(devicePK, tunnelNet)
	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()

	exec := &mockExecutor{}
	clk := clockwork.NewFakeClock()

	// Tenant with VrfId=2 causes vrfNamespaces to include ns-vrf2.
	tenant := serviceability.Tenant{}
	tenant.VrfId = 2
	svc := &mockSvcClient{data: &serviceability.ProgramData{
		Users:   []serviceability.User{user},
		Tenants: []serviceability.Tenant{tenant},
	}}

	// The tunnel (10.0.2.0/31) lives in ns-vrf2 with an ESTABLISHED BGP session.
	iface := makeInterface("tu500", "10.0.2.0/31")
	col := staticEstablishedCollector(
		map[string]map[string]struct{}{
			"ns-vrf1": {},
			"ns-vrf2": {"10.0.2.1": {}}, // peer IP is ESTABLISHED
		},
		map[string][]netutil.Interface{
			"ns-vrf1": nil,
			"ns-vrf2": {iface},
		},
	)

	s := newTestSubmitter(t, clk, exec, svc, col, devicePK, 0, 6*time.Hour, "ns-vrf1", "ns-vrf2")
	s.tick(context.Background())

	s.mu.Lock()
	enqueued := len(s.taskCh)
	s.mu.Unlock()

	if enqueued != 1 {
		t.Fatalf("expected 1 task enqueued, got %d", enqueued)
	}
	task := <-s.taskCh
	if task.status != serviceability.BGPStatusUp {
		t.Errorf("expected Up task, got %v", task.status)
	}
	if solana.PublicKeyFromBytes(task.user.PubKey[:]).String() != userPK {
		t.Errorf("unexpected user in task")
	}
}

// TestTick_MultiNamespace_PartialFailure verifies that when one namespace
// collection fails, tick continues and processes users in the remaining
// namespaces instead of aborting.
func TestTick_MultiNamespace_PartialFailure(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	// User whose tunnel is in ns-vrf1 (the working namespace).
	tunnelNet := [5]byte{10, 0, 1, 0, 31}
	user := makeActivatedUser(devicePK, tunnelNet)
	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()

	exec := &mockExecutor{}
	clk := clockwork.NewFakeClock()

	tenant := serviceability.Tenant{}
	tenant.VrfId = 2
	svc := &mockSvcClient{data: &serviceability.ProgramData{
		Users:   []serviceability.User{user},
		Tenants: []serviceability.Tenant{tenant},
	}}

	// ns-vrf2 fails; ns-vrf1 has the tunnel and an established session.
	iface := makeInterface("tu501", "10.0.1.0/31")
	col := func(_ context.Context, ns string) (map[string]BGPPeerState, []netutil.Interface, error) {
		if ns == "ns-vrf2" {
			return nil, nil, errors.New("namespace unreachable")
		}
		return map[string]BGPPeerState{"10.0.1.1": {Established: true}}, []netutil.Interface{iface}, nil
	}

	s := newTestSubmitter(t, clk, exec, svc, col, devicePK, 0, 6*time.Hour, "ns-vrf1", "ns-vrf2")
	s.tick(context.Background())

	s.mu.Lock()
	enqueued := len(s.taskCh)
	s.mu.Unlock()

	if enqueued != 1 {
		t.Fatalf("expected 1 task enqueued, got %d", enqueued)
	}
	task := <-s.taskCh
	if task.status != serviceability.BGPStatusUp {
		t.Errorf("expected Up task, got %v", task.status)
	}
	if solana.PublicKeyFromBytes(task.user.PubKey[:]).String() != userPK {
		t.Errorf("unexpected user in task")
	}
}

// TestTick_MulticastUser_UsesDefaultNamespace verifies that a multicast user
// whose tunnel lives in the Arista default VRF (exposed as /var/run/netns/default)
// is found and reported Up. Multicast users do not use a per-tenant VRF, so the
// tunnel and BGP session live in the default namespace alongside global routing.
func TestTick_MulticastUser_UsesDefaultNamespace(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	tunnelNet := [5]byte{10, 0, 3, 0, 31} // 10.0.3.0/31

	user := makeMulticastUser(devicePK, tunnelNet)
	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()

	exec := &mockExecutor{}
	clk := clockwork.NewFakeClock()
	svc := &mockSvcClient{data: &serviceability.ProgramData{
		Users: []serviceability.User{user},
	}}

	// The tunnel (10.0.3.0/31) lives in the default namespace with an ESTABLISHED BGP session.
	iface := makeInterface("tu500", "10.0.3.0/31")
	col := staticEstablishedCollector(
		map[string]map[string]struct{}{
			"ns-vrf1": {},
			"default": {"10.0.3.1": {}}, // peer IP is ESTABLISHED in the default VRF
		},
		map[string][]netutil.Interface{
			"ns-vrf1": nil,
			"default": {iface},
		},
	)

	s := newTestSubmitter(t, clk, exec, svc, col, devicePK, 0, 6*time.Hour, "default", "ns-vrf1")
	s.tick(context.Background())

	s.mu.Lock()
	enqueued := len(s.taskCh)
	s.mu.Unlock()

	if enqueued != 1 {
		t.Fatalf("expected 1 task enqueued, got %d", enqueued)
	}
	task := <-s.taskCh
	if task.status != serviceability.BGPStatusUp {
		t.Errorf("expected Up task, got %v", task.status)
	}
	if solana.PublicKeyFromBytes(task.user.PubKey[:]).String() != userPK {
		t.Errorf("unexpected user in task")
	}
}

// TestTick_RTTPlumbedToSubmitTask verifies that the collector's per-peer RttNs
// reaches the submitTask intact, and that a Down task carries zero RTT even when
// the collector previously reported one (no stale RTT after a tunnel disappears).
func TestTick_RTTPlumbedToSubmitTask(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	tunnelNet := [5]byte{10, 0, 4, 0, 31}
	user := makeActivatedUser(devicePK, tunnelNet)

	exec := &mockExecutor{}
	clk := clockwork.NewFakeClock()
	svc := &mockSvcClient{data: &serviceability.ProgramData{Users: []serviceability.User{user}}}

	iface := makeInterface("tu500", "10.0.4.0/31")
	col := staticCollector(
		map[string]map[string]BGPPeerState{
			"ns-vrf1": {"10.0.4.1": {Established: true, RttNs: 5_000_000}}, // 5 ms
		},
		map[string][]netutil.Interface{
			"ns-vrf1": {iface},
		},
	)

	s := newTestSubmitter(t, clk, exec, svc, col, devicePK, 0, 6*time.Hour, "ns-vrf1")
	s.tick(context.Background())

	if len(s.taskCh) != 1 {
		t.Fatalf("expected 1 task enqueued, got %d", len(s.taskCh))
	}
	task := <-s.taskCh
	if task.status != serviceability.BGPStatusUp {
		t.Errorf("expected Up task, got %v", task.status)
	}
	if task.rttNs != 5_000_000 {
		t.Errorf("expected rttNs=5_000_000, got %d", task.rttNs)
	}
}

func TestTick_RTTClearedOnDown(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	tunnelNet := [5]byte{10, 0, 5, 0, 31}
	user := makeActivatedUser(devicePK, tunnelNet)
	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()

	exec := &mockExecutor{}
	clk := clockwork.NewFakeClock()
	svc := &mockSvcClient{data: &serviceability.ProgramData{Users: []serviceability.User{user}}}

	// Tunnel interface exists, but the peer is NOT established (e.g. session bounced).
	// Collector still reports a stale RTT for the peer.
	iface := makeInterface("tu500", "10.0.5.0/31")
	col := staticCollector(
		map[string]map[string]BGPPeerState{
			"ns-vrf1": {"10.0.5.1": {Established: false, RttNs: 9_000_000}},
		},
		map[string][]netutil.Interface{
			"ns-vrf1": {iface},
		},
	)

	s := newTestSubmitter(t, clk, exec, svc, col, devicePK, 0, 6*time.Hour, "ns-vrf1")
	// Pre-seed as Up so a Down transition is enqueued.
	s.mu.Lock()
	s.userState[userPK] = &userState{lastOnchainStatus: serviceability.BGPStatusUp}
	s.mu.Unlock()

	s.tick(context.Background())

	if len(s.taskCh) != 1 {
		t.Fatalf("expected 1 task enqueued, got %d", len(s.taskCh))
	}
	task := <-s.taskCh
	if task.status != serviceability.BGPStatusDown {
		t.Errorf("expected Down task, got %v", task.status)
	}
	if task.rttNs != 0 {
		t.Errorf("expected rttNs=0 on a Down task, got %d", task.rttNs)
	}
}

// TestTick_MultiNamespace_AllFail verifies that when every namespace fails,
// tick aborts and enqueues no tasks.
func TestTick_MultiNamespace_AllFail(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	user := makeActivatedUser(devicePK, [5]byte{10, 0, 0, 0, 31})
	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()

	exec := &mockExecutor{}
	clk := clockwork.NewFakeClock()
	svc := &mockSvcClient{data: &serviceability.ProgramData{Users: []serviceability.User{user}}}

	s := newTestSubmitter(t, clk, exec, svc, errCollector(errors.New("all broken")), devicePK, 0, 6*time.Hour, "ns-vrf1")

	// Pre-seed as Up so we know a Down transition would be attempted if tick ran.
	s.mu.Lock()
	s.userState[userPK] = &userState{lastOnchainStatus: serviceability.BGPStatusUp}
	s.mu.Unlock()

	s.tick(context.Background())

	s.mu.Lock()
	enqueued := len(s.taskCh)
	s.mu.Unlock()

	if enqueued != 0 {
		t.Errorf("expected no tasks when all namespaces fail, got %d", enqueued)
	}
}
