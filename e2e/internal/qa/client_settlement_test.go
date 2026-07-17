package qa

import (
	"errors"
	"testing"

	"github.com/gagliardetto/solana-go"
	shreds "github.com/malbeclabs/doublezero/sdk/shreds/go"
)

func seat(pubkey byte, ipBits uint32, tenure uint16) shreds.KeyedClientSeat {
	var pk solana.PublicKey
	pk[0] = pubkey
	return shreds.KeyedClientSeat{
		Pubkey: pk,
		ClientSeat: shreds.ClientSeat{
			ClientIPBits: ipBits,
			TenureEpochs: tenure,
		},
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

func TestSeatAlreadyWithdrawn(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"seat does not exist", errors.New("seat withdrawal failed: seat account does not exist"), true},
		{"not found", errors.New("client seat not found onchain"), true},
		{"no active seat", errors.New("no active seat for client IP"), true},
		{"nothing to withdraw", errors.New("nothing to withdraw"), true},
		{"case insensitive", errors.New("Seat Does Not Exist"), true},
		{"in-flight rejection is retryable, not success", errors.New("instant seat allocation request is in flight"), false},
		{"transient RPC error is retryable, not success", errors.New("rpc: connection timed out"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := seatAlreadyWithdrawn(tt.err); got != tt.want {
				t.Errorf("seatAlreadyWithdrawn(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
