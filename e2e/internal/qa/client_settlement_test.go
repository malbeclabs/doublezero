package qa

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	shreds "github.com/malbeclabs/doublezero/sdk/shreds/go"
)

func markedPubkey(marker byte) solana.PublicKey {
	var pk solana.PublicKey
	pk[0] = marker
	return pk
}

func seat(pubkey byte, ipBits uint32, tenure uint16) shreds.KeyedClientSeat {
	return shreds.KeyedClientSeat{
		Pubkey: markedPubkey(pubkey),
		ClientSeat: shreds.ClientSeat{
			ClientIPBits: ipBits,
			TenureEpochs: tenure,
		},
	}
}

func TestAvailableShredDeviceKeys(t *testing.T) {
	histories := []shreds.KeyedDeviceHistory{
		{DeviceHistory: shreds.DeviceHistory{
			DeviceKey: markedPubkey(1), Flags: 1 << 1,
			ActiveGrantedSeats: 0, ActiveTotalAvailableSeats: 1,
		}},
		{DeviceHistory: shreds.DeviceHistory{
			DeviceKey: markedPubkey(2), Flags: 1 << 1,
			ActiveGrantedSeats: 0, ActiveTotalAvailableSeats: 0,
		}},
		{DeviceHistory: shreds.DeviceHistory{
			DeviceKey: markedPubkey(3), Flags: 1 << 1,
			ActiveGrantedSeats: 2, ActiveTotalAvailableSeats: 2,
		}},
		{DeviceHistory: shreds.DeviceHistory{
			DeviceKey:          markedPubkey(4),
			ActiveGrantedSeats: 0, ActiveTotalAvailableSeats: 1,
		}},
	}

	available := availableShredDeviceKeys(histories)
	if len(available) != 1 || !available[markedPubkey(1).String()] {
		t.Fatalf("availableShredDeviceKeys() = %v, want only device 1", available)
	}
}

func TestClosestAvailableDeviceInMetros(t *testing.T) {
	closestUnavailable := &Device{
		PubKey: markedPubkey(1).String(), Code: "closest-unavailable", ExchangePubKey: "retransmit",
	}
	qaBypassDevice := &Device{
		// Pending with max_users=0: the QA pubkey bypasses these serviceability
		// checks, so shred capacity is the only capacity filter this selector adds.
		PubKey: markedPubkey(2).String(), Code: "qa-bypass", ExchangePubKey: "retransmit",
	}
	fartherAvailable := &Device{
		PubKey: markedPubkey(3).String(), Code: "available", ExchangePubKey: "retransmit",
	}
	wrongMetro := &Device{
		PubKey: markedPubkey(4).String(), Code: "wrong-metro", ExchangePubKey: "other",
	}
	devices := map[string]*Device{
		closestUnavailable.Code: closestUnavailable,
		qaBypassDevice.Code:     qaBypassDevice,
		fartherAvailable.Code:   fartherAvailable,
		wrongMetro.Code:         wrongMetro,
	}
	latencies := []*pb.Latency{
		{DeviceCode: closestUnavailable.Code, Reachable: true, AvgLatencyNs: 1},
		{DeviceCode: qaBypassDevice.Code, Reachable: true, AvgLatencyNs: 2},
		{DeviceCode: wrongMetro.Code, Reachable: true, AvgLatencyNs: 3},
		{DeviceCode: fartherAvailable.Code, Reachable: true, AvgLatencyNs: 4},
	}
	available := map[string]bool{
		qaBypassDevice.PubKey:   true,
		wrongMetro.PubKey:       true,
		fartherAvailable.PubKey: true,
	}

	got, avg := closestAvailableDeviceInMetros(latencies, devices, map[string]bool{"retransmit": true}, available, true)
	if got != qaBypassDevice || avg != 2 {
		t.Fatalf("closestAvailableDeviceInMetros() = (%v, %d), want (%v, 2)", got, avg, qaBypassDevice)
	}
}

func TestFilterActiveSeats(t *testing.T) {
	const ip = uint32(0x0A000001)      // 10.0.0.1
	const otherIP = uint32(0x0A000002) // 10.0.0.2

	tests := []struct {
		name  string
		seats []shreds.KeyedClientSeat
		want  []byte // expected pubkey[0] markers, in order
	}{
		{
			name:  "empty",
			seats: nil,
			want:  nil,
		},
		{
			name: "active seat for our IP is selected",
			seats: []shreds.KeyedClientSeat{
				seat(1, ip, 1),
			},
			want: []byte{1},
		},
		{
			name: "inactive seat (tenure 0) for our IP is skipped",
			seats: []shreds.KeyedClientSeat{
				seat(1, ip, 0),
			},
			want: nil,
		},
		{
			name: "active seat for a different IP is skipped",
			seats: []shreds.KeyedClientSeat{
				seat(1, otherIP, 3),
			},
			want: nil,
		},
		{
			name: "active seats on multiple devices for our IP are all selected",
			seats: []shreds.KeyedClientSeat{
				seat(1, ip, 1),
				seat(2, otherIP, 2), // different IP, skip
				seat(3, ip, 5),      // active on another device, keep
				seat(4, ip, 0),      // our IP but withdrawn, skip
			},
			want: []byte{1, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterActiveSeats(tt.seats, ip)
			if len(got) != len(tt.want) {
				t.Fatalf("filterActiveSeats returned %d seats, want %d", len(got), len(tt.want))
			}
			for i, marker := range tt.want {
				if got[i].Pubkey[0] != marker {
					t.Errorf("seat[%d].Pubkey[0] = %d, want %d", i, got[i].Pubkey[0], marker)
				}
				if got[i].TenureEpochs == 0 {
					t.Errorf("seat[%d] has TenureEpochs == 0, should have been filtered out", i)
				}
				if got[i].ClientIPBits != ip {
					t.Errorf("seat[%d].ClientIPBits = %x, want %x", i, got[i].ClientIPBits, ip)
				}
			}
		})
	}
}

func TestIsAccountNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"shreds account-not-found sentinel", shreds.ErrAccountNotFound, true},
		{"rpc not-found sentinel", rpc.ErrNotFound, true},
		{"wrapped shreds sentinel", fmt.Errorf("deriving client seat PDA: %w", shreds.ErrAccountNotFound), true},
		{"wrapped rpc sentinel", fmt.Errorf("fetching account: %w", rpc.ErrNotFound), true},
		// The whole point of matching sentinels rather than error text: a
		// transient "Blockhash not found" must NOT read as a missing seat, else
		// a failed withdraw would be silently treated as already-withdrawn.
		{"blockhash not found is not a missing account", errors.New("Transaction simulation failed: Blockhash not found"), false},
		{"jsonrpc method not found is not a missing account", errors.New("rpc: Method not found"), false},
		{"generic error", errors.New("connection timed out"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAccountNotFound(tt.err); got != tt.want {
				t.Errorf("isAccountNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsInFlightPreflightBail(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"in flight (spaced)", errors.New("seat withdrawal failed: instant seat allocation request is in flight"), true},
		{"in-flight (hyphen)", errors.New("request is in-flight, cannot withdraw"), true},
		{"case insensitive", errors.New("Request In Flight"), true},
		// A submission timeout must NOT rotate endpoints: the tx may have landed.
		{"submission timeout does not rotate", errors.New("Transaction was not confirmed in 30s: Blockhash not found"), false},
		{"generic rpc error does not rotate", errors.New("rpc: connection timed out"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInFlightPreflightBail(tt.err); got != tt.want {
				t.Errorf("isInFlightPreflightBail(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
