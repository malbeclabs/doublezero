//go:build linux

package uping

import (
	"encoding/binary"
	"math/rand"
	"testing"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// Must not panic on arbitrary bytes (IPv4 or bare ICMP).
func FuzzUping_ValidateEchoReply_Malformed_NoPanic(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x45})                // minimal header byte
	f.Add(make([]byte, 19))            // truncated IPv4
	f.Add([]byte{8, 0, 0, 0, 0, 0, 0}) // short ICMP
	f.Fuzz(func(t *testing.T, pkt []byte) {
		if len(pkt) > 1<<16 {
			pkt = pkt[:1<<16]
		}
		_, _, _, _ = validateEchoReply(pkt, 0xBEEF, 1, 99)
	})
}

// ICMP checksum property: set -> validates to zero; flip a byte -> non-zero.
func FuzzUping_ICMPChecksum_Roundtrip(f *testing.F) {
	seed := fuzzEchoReply(0x1234, 7, 42, 8)
	f.Add(seed)
	f.Fuzz(func(t *testing.T, msg []byte) {
		if len(msg) < 8 {
			msg = append(msg, make([]byte, 8-len(msg))...)
		}
		if len(msg) > 2048 {
			msg = msg[:2048]
		}
		binary.BigEndian.PutUint16(msg[2:], 0)
		cs := icmpChecksum(msg)
		binary.BigEndian.PutUint16(msg[2:], cs)
		if icmpChecksum(msg) != 0 {
			t.Fatalf("checksum not zero after set")
		}
		if len(msg) > 8 {
			i := 8 + rand.Intn(len(msg)-8)
			msg[i] ^= 0xFF
			if icmpChecksum(msg) == 0 {
				t.Fatalf("checksum still zero after flip")
			}
		}
	})
}

// Bare helper: valid Echo Reply bytes.
func fuzzEchoReply(id, seq uint16, nonce uint64, extra int) []byte {
	if extra < 0 {
		extra = -extra
	}
	if extra > 1024 {
		extra = 1024
	}
	data := make([]byte, 8+extra)
	binary.BigEndian.PutUint64(data[:8], nonce)
	msg := &icmp.Message{
		Type: ipv4.ICMPTypeEchoReply, Code: 0,
		Body: &icmp.Echo{ID: int(id), Seq: int(seq), Data: data},
	}
	b, _ := msg.Marshal(nil)
	return b
}
