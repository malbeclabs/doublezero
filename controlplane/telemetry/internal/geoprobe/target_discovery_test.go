package geoprobe

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/gagliardetto/solana-go"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
)

// mockGeolocationUserClient implements GeolocationUserClient for testing.
type mockGeolocationUserClient struct {
	users []geolocation.KeyedGeolocationUser
	err   error
	calls int
}

func (m *mockGeolocationUserClient) GetGeolocationUsers(_ context.Context) ([]geolocation.KeyedGeolocationUser, error) {
	m.calls++
	return m.users, m.err
}

func testProbePubkey() solana.PublicKey {
	var pk solana.PublicKey
	pk[0] = 1
	pk[31] = 1
	return pk
}

func otherProbePubkey() solana.PublicKey {
	var pk solana.PublicKey
	pk[0] = 2
	pk[31] = 2
	return pk
}

func makeUser(status geolocation.GeolocationUserStatus, payment geolocation.GeolocationPaymentStatus, code string, targets []geolocation.GeolocationTarget) geolocation.KeyedGeolocationUser {
	return geolocation.KeyedGeolocationUser{
		Pubkey: solana.NewWallet().PublicKey(),
		GeolocationUser: geolocation.GeolocationUser{
			AccountType:   geolocation.AccountTypeGeolocationUser,
			Status:        status,
			PaymentStatus: payment,
			Code:          code,
			Targets:       targets,
		},
	}
}

func makeUserWithResultDest(status geolocation.GeolocationUserStatus, payment geolocation.GeolocationPaymentStatus, code string, targets []geolocation.GeolocationTarget, resultDest string) geolocation.KeyedGeolocationUser {
	return geolocation.KeyedGeolocationUser{
		Pubkey: solana.NewWallet().PublicKey(),
		GeolocationUser: geolocation.GeolocationUser{
			AccountType:       geolocation.AccountTypeGeolocationUser,
			Status:            status,
			PaymentStatus:     payment,
			Code:              code,
			Targets:           targets,
			ResultDestination: resultDest,
		},
	}
}

func outboundTarget(ip [4]uint8, port uint16, probePK solana.PublicKey) geolocation.GeolocationTarget {
	return geolocation.GeolocationTarget{
		TargetType:         geolocation.GeoLocationTargetTypeOutbound,
		IPAddress:          ip,
		LocationOffsetPort: port,
		GeoProbePK:         probePK,
	}
}

func inboundTarget(targetPK solana.PublicKey, probePK solana.PublicKey) geolocation.GeolocationTarget {
	return geolocation.GeolocationTarget{
		TargetType: geolocation.GeoLocationTargetTypeInbound,
		TargetPK:   targetPK,
		GeoProbePK: probePK,
	}
}

func outboundIcmpTarget(ip [4]uint8, port uint16, probePK solana.PublicKey) geolocation.GeolocationTarget {
	return geolocation.GeolocationTarget{
		TargetType:         geolocation.GeoLocationTargetTypeOutboundIcmp,
		IPAddress:          ip,
		LocationOffsetPort: port,
		GeoProbePK:         probePK,
	}
}

func newTestTargetDiscovery(client GeolocationUserClient) *TargetDiscovery {
	td, _ := NewTargetDiscovery(&TargetDiscoveryConfig{
		GeoProbePubkey: testProbePubkey(),
		Client:         client,
		Logger:         slog.Default(),
	})
	return td
}

func TestTargetDiscovery_HappyPath(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, _, keys, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Host != "44.0.0.1" || targets[0].Port != 9000 {
		t.Errorf("unexpected target: %v", targets[0])
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 inbound keys, got %d", len(keys))
	}
}

func TestTargetDiscovery_StatusFilter(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusSuspended, geolocation.GeolocationPaymentStatusPaid, "suspended", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, _, keys, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected 0 targets for suspended user, got %d", len(targets))
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys for suspended user, got %d", len(keys))
	}
}

func TestTargetDiscovery_PaymentFilter(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusDelinquent, "delinquent", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, _, keys, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected 0 targets for delinquent user, got %d", len(targets))
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys for delinquent user, got %d", len(keys))
	}
}

func TestTargetDiscovery_CombinedFilter(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusSuspended, geolocation.GeolocationPaymentStatusDelinquent, "bad", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
			}),
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "good", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 2}, 9001, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, _, _, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Host != "44.0.0.2" {
		t.Errorf("expected 10.0.0.2, got %s", targets[0].Host)
	}
}

func TestTargetDiscovery_ProbePKFilter(t *testing.T) {
	otherPK := otherProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 1}, 9000, otherPK),
			}),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, _, _, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected 0 targets for other probe, got %d", len(targets))
	}
}

func TestTargetDiscovery_InboundTargets(t *testing.T) {
	probePK := testProbePubkey()
	targetPK := solana.NewWallet().PublicKey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				inboundTarget(targetPK, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, _, keys, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected 0 outbound targets, got %d", len(targets))
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 inbound key, got %d", len(keys))
	}
	var expectedKey [32]byte
	copy(expectedKey[:], targetPK[:])
	if keys[0] != expectedKey {
		t.Errorf("unexpected inbound key")
	}
}

func TestTargetDiscovery_MixedTargets(t *testing.T) {
	probePK := testProbePubkey()
	targetPK := solana.NewWallet().PublicKey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
				inboundTarget(targetPK, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, _, keys, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 outbound target, got %d", len(targets))
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 inbound key, got %d", len(keys))
	}
}

func TestTargetDiscovery_DiffDetection(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client)
	targetCh := make(chan TargetUpdate, 2)
	keyCh := make(chan InboundKeyUpdate, 2)
	icmpTargetCh := make(chan ICMPTargetUpdate, 2)

	ctx := context.Background()
	// First call should send update.
	td.discoverAndSend(ctx, targetCh, keyCh, icmpTargetCh)
	if len(targetCh) != 1 {
		t.Fatalf("expected 1 target update after first call, got %d", len(targetCh))
	}
	<-targetCh

	// Second call with same data should not send update.
	td.discoverAndSend(ctx, targetCh, keyCh, icmpTargetCh)
	if len(targetCh) != 0 {
		t.Errorf("expected no target update for unchanged data, got %d", len(targetCh))
	}
}

func TestTargetDiscovery_RPCError(t *testing.T) {
	client := &mockGeolocationUserClient{
		err: fmt.Errorf("rpc unavailable"),
	}

	td := newTestTargetDiscovery(client)
	targetCh := make(chan TargetUpdate, 1)
	keyCh := make(chan InboundKeyUpdate, 1)
	icmpTargetCh := make(chan ICMPTargetUpdate, 1)

	td.discoverAndSend(context.Background(), targetCh, keyCh, icmpTargetCh)
	if len(targetCh) != 0 {
		t.Errorf("expected no update on RPC error")
	}
}

func TestTargetDiscovery_DeduplicateInboundKeys(t *testing.T) {
	probePK := testProbePubkey()
	targetPK := solana.NewWallet().PublicKey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				inboundTarget(targetPK, probePK),
			}),
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user2", []geolocation.GeolocationTarget{
				inboundTarget(targetPK, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client)
	_, _, keys, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("expected 1 deduplicated key, got %d", len(keys))
	}
}

func TestNewTargetDiscovery_Validation(t *testing.T) {
	logger := slog.Default()
	client := &mockGeolocationUserClient{}
	probePK := testProbePubkey()

	tests := []struct {
		name string
		cfg  *TargetDiscoveryConfig
	}{
		{"nil logger", &TargetDiscoveryConfig{Client: client, GeoProbePubkey: probePK}},
		{"nil client", &TargetDiscoveryConfig{Logger: logger, GeoProbePubkey: probePK}},
		{"zero pubkey", &TargetDiscoveryConfig{Logger: logger, Client: client}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewTargetDiscovery(tt.cfg)
			if err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestTargetDiscovery_TargetUpdateCountUnchanged_SkipsScan(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	var counter atomic.Uint32
	counter.Store(5)

	td, _ := NewTargetDiscovery(&TargetDiscoveryConfig{
		GeoProbePubkey:         testProbePubkey(),
		Client:                 client,
		Logger:                 slog.Default(),
		ProbeTargetUpdateCount: &counter,
	})

	// First call (tick 0): always does full scan (forceFullRefresh).
	targets, _, _, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target on first scan, got %d", len(targets))
	}
	if client.calls != 1 {
		t.Fatalf("expected 1 RPC call on first scan, got %d", client.calls)
	}

	// Second call: counter unchanged → should skip.
	targets, _, keys, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targets != nil || keys != nil {
		t.Errorf("expected nil targets/keys when skipped, got targets=%v keys=%v", targets, keys)
	}
	if client.calls != 1 {
		t.Errorf("expected no additional RPC call when skipped, got %d total", client.calls)
	}
}

func TestTargetDiscovery_TargetUpdateCountChanged_DoesFullScan(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	var counter atomic.Uint32
	counter.Store(5)

	td, _ := NewTargetDiscovery(&TargetDiscoveryConfig{
		GeoProbePubkey:         testProbePubkey(),
		Client:                 client,
		Logger:                 slog.Default(),
		ProbeTargetUpdateCount: &counter,
	})

	// First call: full scan.
	_, _, _, _, _, _ = td.discover(context.Background())

	// Change counter, second call should do full scan.
	counter.Store(6)
	targets, _, _, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target after counter change, got %d", len(targets))
	}
	if client.calls != 2 {
		t.Errorf("expected 2 RPC calls total, got %d", client.calls)
	}
}

func TestTargetDiscovery_ForcedFullRefresh_IgnoresCounter(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	var counter atomic.Uint32
	counter.Store(5)

	td, _ := NewTargetDiscovery(&TargetDiscoveryConfig{
		GeoProbePubkey:         testProbePubkey(),
		Client:                 client,
		Logger:                 slog.Default(),
		ProbeTargetUpdateCount: &counter,
	})

	// Tick through to the next forced refresh (every 5th tick).
	// Tick 0: forced (0 % 5 == 0), tick 1-4: skipped (counter unchanged), tick 5: forced.
	for i := 0; i < targetDiscoveryFullRefreshEvery; i++ {
		_, _, _, _, _, _ = td.discover(context.Background())
	}
	callsBefore := client.calls

	// Next tick (tick 5): forced full refresh even though counter unchanged.
	targets, _, _, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target on forced refresh, got %d", len(targets))
	}
	if client.calls != callsBefore+1 {
		t.Errorf("expected forced refresh to call RPC, calls before=%d after=%d", callsBefore, client.calls)
	}
}

func TestTargetDiscovery_NilProbeTargetUpdateCount_AlwaysScans(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	// No ProbeTargetUpdateCount set — backward compat: always scans.
	td := newTestTargetDiscovery(client)

	for i := 0; i < 3; i++ {
		_, _, _, _, _, _ = td.discover(context.Background())
	}
	if client.calls != 3 {
		t.Errorf("expected 3 RPC calls without ProbeTargetUpdateCount, got %d", client.calls)
	}
}

func TestTargetDiscovery_RejectsNonPublicOutboundTargets(t *testing.T) {
	probePK := testProbePubkey()

	tests := []struct {
		name string
		ip   [4]uint8
	}{
		{"loopback", [4]uint8{127, 0, 0, 1}},
		{"private 10/8", [4]uint8{10, 0, 0, 1}},
		{"private 172.16/12", [4]uint8{172, 16, 0, 1}},
		{"private 192.168/16", [4]uint8{192, 168, 1, 1}},
		{"link-local", [4]uint8{169, 254, 1, 1}},
		{"multicast", [4]uint8{224, 0, 0, 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockGeolocationUserClient{
				users: []geolocation.KeyedGeolocationUser{
					makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
						outboundTarget(tt.ip, 9000, probePK),
					}),
				},
			}
			td := newTestTargetDiscovery(client)
			targets, _, _, _, _, err := td.discover(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(targets) != 0 {
				t.Errorf("expected non-public target %v to be rejected, got %v", tt.ip, targets)
			}
		})
	}
}

func TestTargetDiscovery_OutboundIcmpTargets(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundIcmpTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, icmpTargets, keys, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected 0 TWAMP targets, got %d", len(targets))
	}
	if len(icmpTargets) != 1 {
		t.Fatalf("expected 1 ICMP target, got %d", len(icmpTargets))
	}
	if icmpTargets[0].Host != "44.0.0.1" || icmpTargets[0].Port != 9000 {
		t.Errorf("unexpected ICMP target: %v", icmpTargets[0])
	}
	if icmpTargets[0].TWAMPPort != 0 {
		t.Errorf("expected TWAMPPort=0 for ICMP target, got %d", icmpTargets[0].TWAMPPort)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 inbound keys, got %d", len(keys))
	}
}

func TestTargetDiscovery_MixedOutboundAndIcmp(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
				outboundIcmpTarget([4]uint8{44, 0, 0, 2}, 9001, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, icmpTargets, keys, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 TWAMP target, got %d", len(targets))
	}
	if targets[0].Host != "44.0.0.1" {
		t.Errorf("unexpected TWAMP target host: %s", targets[0].Host)
	}
	if len(icmpTargets) != 1 {
		t.Fatalf("expected 1 ICMP target, got %d", len(icmpTargets))
	}
	if icmpTargets[0].Host != "44.0.0.2" {
		t.Errorf("unexpected ICMP target host: %s", icmpTargets[0].Host)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

func TestTargetDiscovery_OutboundIcmpZeroPortDefaulted(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUserWithResultDest(
				geolocation.GeolocationUserStatusActivated,
				geolocation.GeolocationPaymentStatusPaid,
				"user1",
				[]geolocation.GeolocationTarget{
					outboundIcmpTarget([4]uint8{44, 0, 0, 1}, 0, probePK),
				},
				"results.example.com:9000",
			),
		},
	}

	td := newTestTargetDiscovery(client)
	_, icmpTargets, _, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(icmpTargets) != 1 {
		t.Fatalf("expected 1 ICMP target (zero port should default, not be rejected), got %d", len(icmpTargets))
	}
	if icmpTargets[0].Port != telemetryconfig.DefaultGeoprobeUDPPort {
		t.Errorf("expected Port=%d (default), got %d", telemetryconfig.DefaultGeoprobeUDPPort, icmpTargets[0].Port)
	}
}

func TestTargetDiscovery_OutboundIcmpPrivateIPRejected(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundIcmpTarget([4]uint8{10, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client)
	_, icmpTargets, _, _, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(icmpTargets) != 0 {
		t.Errorf("expected private IP to be rejected, got %d targets", len(icmpTargets))
	}
}

func TestTargetDiscovery_ResultDestination_OutboundOverride(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUserWithResultDest(
				geolocation.GeolocationUserStatusActivated,
				geolocation.GeolocationPaymentStatusPaid,
				"user1",
				[]geolocation.GeolocationTarget{
					outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
				},
				"185.199.108.1:9000",
			),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, _, _, delivery, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if len(delivery) != 1 {
		t.Fatalf("expected 1 delivery override, got %d", len(delivery))
	}
	dest, ok := delivery[targets[0]]
	if !ok {
		t.Fatal("expected delivery address for target")
	}
	if dest != "185.199.108.1:9000" {
		t.Errorf("expected delivery 185.199.108.1:9000, got %s", dest)
	}
}

func TestTargetDiscovery_NoResultDestination_NoDeliveryOverride(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(
				geolocation.GeolocationUserStatusActivated,
				geolocation.GeolocationPaymentStatusPaid,
				"user1",
				[]geolocation.GeolocationTarget{
					outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
				},
			),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, _, _, delivery, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if len(delivery) != 0 {
		t.Errorf("expected no delivery overrides for user without result destination, got %d", len(delivery))
	}
}

func TestTargetDiscovery_ResultDestination_ICMPOverride(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUserWithResultDest(
				geolocation.GeolocationUserStatusActivated,
				geolocation.GeolocationPaymentStatusPaid,
				"user1",
				[]geolocation.GeolocationTarget{
					outboundIcmpTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
				},
				"results.example.com:9000",
			),
		},
	}

	td := newTestTargetDiscovery(client)
	_, icmpTargets, _, _, delivery, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(icmpTargets) != 1 {
		t.Fatalf("expected 1 ICMP target, got %d", len(icmpTargets))
	}
	dest, ok := delivery[icmpTargets[0]]
	if !ok {
		t.Fatal("expected delivery address for ICMP target")
	}
	if dest != "results.example.com:9000" {
		t.Errorf("expected delivery results.example.com:9000, got %s", dest)
	}
}

func TestTargetDiscovery_ResultDestination_MixedUsers(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			// User with result destination.
			makeUserWithResultDest(
				geolocation.GeolocationUserStatusActivated,
				geolocation.GeolocationPaymentStatusPaid,
				"user-with-dest",
				[]geolocation.GeolocationTarget{
					outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
				},
				"185.199.108.1:9000",
			),
			// User without result destination.
			makeUser(
				geolocation.GeolocationUserStatusActivated,
				geolocation.GeolocationPaymentStatusPaid,
				"user-no-dest",
				[]geolocation.GeolocationTarget{
					outboundTarget([4]uint8{44, 0, 0, 2}, 9001, probePK),
				},
			),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, _, _, delivery, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	if len(delivery) != 1 {
		t.Fatalf("expected 1 delivery override, got %d", len(delivery))
	}

	// Find the target with the delivery override.
	addr1 := ProbeAddress{Host: "44.0.0.1", Port: 9000, TWAMPPort: 8925}
	addr2 := ProbeAddress{Host: "44.0.0.2", Port: 9001, TWAMPPort: 8925}

	if dest, ok := delivery[addr1]; !ok || dest != "185.199.108.1:9000" {
		t.Errorf("expected delivery override for addr1, got ok=%v dest=%q", ok, dest)
	}
	if _, ok := delivery[addr2]; ok {
		t.Error("expected no delivery override for addr2")
	}
}

func TestTargetDiscovery_ResultDestination_DomainName(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUserWithResultDest(
				geolocation.GeolocationUserStatusActivated,
				geolocation.GeolocationPaymentStatusPaid,
				"user1",
				[]geolocation.GeolocationTarget{
					outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
				},
				"results.example.com:9000",
			),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, _, _, delivery, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	dest, ok := delivery[targets[0]]
	if !ok {
		t.Fatal("expected delivery address for target")
	}
	if dest != "results.example.com:9000" {
		t.Errorf("expected domain delivery address, got %s", dest)
	}
}

func TestTargetDiscovery_DeliveryAddrsChangeTriggersUpdate(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUserWithResultDest(
				geolocation.GeolocationUserStatusActivated,
				geolocation.GeolocationPaymentStatusPaid,
				"user1",
				[]geolocation.GeolocationTarget{
					outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
				},
				"185.199.108.1:9000",
			),
		},
	}

	td := newTestTargetDiscovery(client)
	targetCh := make(chan TargetUpdate, 2)
	keyCh := make(chan InboundKeyUpdate, 2)
	icmpTargetCh := make(chan ICMPTargetUpdate, 2)

	ctx := context.Background()

	// First call should send update.
	td.discoverAndSend(ctx, targetCh, keyCh, icmpTargetCh)
	if len(targetCh) != 1 {
		t.Fatalf("expected 1 target update after first call, got %d", len(targetCh))
	}
	update := <-targetCh
	if len(update.DeliveryAddrs) != 1 {
		t.Fatalf("expected 1 delivery addr in update, got %d", len(update.DeliveryAddrs))
	}

	// Same data — no update.
	td.discoverAndSend(ctx, targetCh, keyCh, icmpTargetCh)
	if len(targetCh) != 0 {
		t.Errorf("expected no update for unchanged data, got %d", len(targetCh))
	}

	// Change delivery address — should trigger update even though targets are the same.
	client.users[0].GeolocationUser.ResultDestination = "44.0.0.99:8080"
	td.discoverAndSend(ctx, targetCh, keyCh, icmpTargetCh)
	if len(targetCh) != 1 {
		t.Fatalf("expected 1 target update after delivery change, got %d", len(targetCh))
	}
	update = <-targetCh
	addr := ProbeAddress{Host: "44.0.0.1", Port: 9000, TWAMPPort: 8925}
	if update.DeliveryAddrs[addr] != "44.0.0.99:8080" {
		t.Errorf("expected updated delivery address, got %s", update.DeliveryAddrs[addr])
	}
}

func TestTargetDiscovery_InvalidResultDestination_Ignored(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUserWithResultDest(
				geolocation.GeolocationUserStatusActivated,
				geolocation.GeolocationPaymentStatusPaid,
				"user1",
				[]geolocation.GeolocationTarget{
					outboundTarget([4]uint8{44, 0, 0, 1}, 9000, probePK),
				},
				"not-a-valid-address", // missing port
			),
		},
	}

	td := newTestTargetDiscovery(client)
	targets, _, _, delivery, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target (address still valid), got %d", len(targets))
	}
	if len(delivery) != 0 {
		t.Errorf("expected no delivery overrides for invalid result destination, got %d", len(delivery))
	}
}
