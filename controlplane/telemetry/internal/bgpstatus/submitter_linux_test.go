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
func staticCollector(
	established map[string]map[string]struct{},
	ifaces map[string][]netutil.Interface,
) NamespaceCollector {
	return func(_ context.Context, ns string) (map[string]struct{}, []netutil.Interface, error) {
		return established[ns], ifaces[ns], nil
	}
}

// errCollector returns a NamespaceCollector that always fails.
func errCollector(err error) NamespaceCollector {
	return func(_ context.Context, _ string) (map[string]struct{}, []netutil.Interface, error) {
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
	col := staticCollector(
		map[string]map[string]struct{}{"ns-vrf1": {}},
		map[string][]netutil.Interface{"ns-vrf1": nil},
	)

	s := newTestSubmitter(t, clk, exec, svc, col, devicePK, 0, 6*time.Hour)

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
	col := staticCollector(
		map[string]map[string]struct{}{
			"ns-vrf1": {},
			"ns-vrf2": {"10.0.2.1": {}}, // peer IP is ESTABLISHED
		},
		map[string][]netutil.Interface{
			"ns-vrf1": nil,
			"ns-vrf2": {iface},
		},
	)

	s := newTestSubmitter(t, clk, exec, svc, col, devicePK, 0, 6*time.Hour)
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
	col := func(_ context.Context, ns string) (map[string]struct{}, []netutil.Interface, error) {
		if ns == "ns-vrf2" {
			return nil, nil, errors.New("namespace unreachable")
		}
		return map[string]struct{}{"10.0.1.1": {}}, []netutil.Interface{iface}, nil
	}

	s := newTestSubmitter(t, clk, exec, svc, col, devicePK, 0, 6*time.Hour)
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

// TestTick_MultiNamespace_AllFail verifies that when every namespace fails,
// tick aborts and enqueues no tasks.
func TestTick_MultiNamespace_AllFail(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	user := makeActivatedUser(devicePK, [5]byte{10, 0, 0, 0, 31})
	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()

	exec := &mockExecutor{}
	clk := clockwork.NewFakeClock()
	svc := &mockSvcClient{data: &serviceability.ProgramData{Users: []serviceability.User{user}}}

	s := newTestSubmitter(t, clk, exec, svc, errCollector(errors.New("all broken")), devicePK, 0, 6*time.Hour)

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
