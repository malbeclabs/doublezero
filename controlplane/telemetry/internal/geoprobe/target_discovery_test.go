package geoprobe

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
)

// mockGeolocationUserClient implements GeolocationUserClient for testing.
type mockGeolocationUserClient struct {
	users []geolocation.KeyedGeolocationUser
	err   error
}

func (m *mockGeolocationUserClient) GetGeolocationUsers(_ context.Context) ([]geolocation.KeyedGeolocationUser, error) {
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

func newTestTargetDiscovery(client GeolocationUserClient, cliTargets []ProbeAddress, cliKeys [][32]byte) *TargetDiscovery {
	td, _ := NewTargetDiscovery(&TargetDiscoveryConfig{
		GeoProbePubkey: testProbePubkey(),
		Client:         client,
		CLITargets:     cliTargets,
		CLIAllowedKeys: cliKeys,
		Interval:       time.Minute,
		Logger:         slog.Default(),
	})
	return td
}

func TestTargetDiscovery_HappyPath(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{10, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client, nil, nil)
	targets, keys, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Host != "10.0.0.1" || targets[0].Port != 9000 {
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
				outboundTarget([4]uint8{10, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client, nil, nil)
	targets, keys, err := td.discover(context.Background())
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
				outboundTarget([4]uint8{10, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client, nil, nil)
	targets, keys, err := td.discover(context.Background())
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
				outboundTarget([4]uint8{10, 0, 0, 1}, 9000, probePK),
			}),
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "good", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{10, 0, 0, 2}, 9001, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client, nil, nil)
	targets, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Host != "10.0.0.2" {
		t.Errorf("expected 10.0.0.2, got %s", targets[0].Host)
	}
}

func TestTargetDiscovery_ProbePKFilter(t *testing.T) {
	otherPK := otherProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{10, 0, 0, 1}, 9000, otherPK),
			}),
		},
	}

	td := newTestTargetDiscovery(client, nil, nil)
	targets, _, err := td.discover(context.Background())
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

	td := newTestTargetDiscovery(client, nil, nil)
	targets, keys, err := td.discover(context.Background())
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
				outboundTarget([4]uint8{10, 0, 0, 1}, 9000, probePK),
				inboundTarget(targetPK, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client, nil, nil)
	targets, keys, err := td.discover(context.Background())
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

func TestTargetDiscovery_MergeWithCLITargets(t *testing.T) {
	probePK := testProbePubkey()
	cliTargets := []ProbeAddress{{Host: "10.0.0.1", Port: 9000, TWAMPPort: 8925}}
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{10, 0, 0, 2}, 9001, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client, cliTargets, nil)
	targets, _, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 merged targets, got %d", len(targets))
	}
}

func TestTargetDiscovery_MergeWithCLIKeys(t *testing.T) {
	probePK := testProbePubkey()
	var cliKey [32]byte
	cliKey[0] = 99
	targetPK := solana.NewWallet().PublicKey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				inboundTarget(targetPK, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client, nil, [][32]byte{cliKey})
	_, keys, err := td.discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 merged keys, got %d", len(keys))
	}
}

func TestTargetDiscovery_DiffDetection(t *testing.T) {
	probePK := testProbePubkey()
	client := &mockGeolocationUserClient{
		users: []geolocation.KeyedGeolocationUser{
			makeUser(geolocation.GeolocationUserStatusActivated, geolocation.GeolocationPaymentStatusPaid, "user1", []geolocation.GeolocationTarget{
				outboundTarget([4]uint8{10, 0, 0, 1}, 9000, probePK),
			}),
		},
	}

	td := newTestTargetDiscovery(client, nil, nil)
	targetCh := make(chan TargetUpdate, 2)
	keyCh := make(chan InboundKeyUpdate, 2)

	ctx := context.Background()
	// First call should send update.
	td.discoverAndSend(ctx, targetCh, keyCh)
	if len(targetCh) != 1 {
		t.Fatalf("expected 1 target update after first call, got %d", len(targetCh))
	}
	<-targetCh

	// Second call with same data should not send update.
	td.discoverAndSend(ctx, targetCh, keyCh)
	if len(targetCh) != 0 {
		t.Errorf("expected no target update for unchanged data, got %d", len(targetCh))
	}
}

func TestTargetDiscovery_RPCError(t *testing.T) {
	client := &mockGeolocationUserClient{
		err: fmt.Errorf("rpc unavailable"),
	}

	td := newTestTargetDiscovery(client, nil, nil)
	targetCh := make(chan TargetUpdate, 1)
	keyCh := make(chan InboundKeyUpdate, 1)

	td.discoverAndSend(context.Background(), targetCh, keyCh)
	if len(targetCh) != 0 {
		t.Errorf("expected no update on RPC error")
	}
}

func TestTargetDiscovery_ContextCancellation(t *testing.T) {
	client := &mockGeolocationUserClient{}
	td := newTestTargetDiscovery(client, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	targetCh := make(chan TargetUpdate, 1)
	keyCh := make(chan InboundKeyUpdate, 1)

	done := make(chan struct{})
	go func() {
		td.Run(ctx, targetCh, keyCh)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
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

	td := newTestTargetDiscovery(client, nil, nil)
	_, keys, err := td.discover(context.Background())
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
		{"nil logger", &TargetDiscoveryConfig{Client: client, GeoProbePubkey: probePK, Interval: time.Minute}},
		{"nil client", &TargetDiscoveryConfig{Logger: logger, GeoProbePubkey: probePK, Interval: time.Minute}},
		{"zero pubkey", &TargetDiscoveryConfig{Logger: logger, Client: client, Interval: time.Minute}},
		{"zero interval", &TargetDiscoveryConfig{Logger: logger, Client: client, GeoProbePubkey: probePK}},
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
