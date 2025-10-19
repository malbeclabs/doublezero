//go:build linux

package uping

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Ensures validateEchoReply never panics on arbitrary input.
func FuzzUping_ValidateEchoReply_Malformed_NoPanic(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x45, 0x00})
	f.Add(make([]byte, 19))
	f.Fuzz(func(t *testing.T, pkt []byte) {
		if len(pkt) > 1<<16 {
			pkt = pkt[:1<<16]
		}
		validateEchoReply(pkt, 0xBEEF, 1, 99)
	})
}

// Verifies ensureICMPBuf correctly sizes and reuses backing storage when large enough.
func FuzzUping_EnsureICMPBuf(f *testing.F) {
	f.Add(0, 0)
	f.Add(32, 8)
	f.Fuzz(func(t *testing.T, capHint, payloadLen int) {
		if capHint < 0 {
			capHint = -capHint
		}
		if payloadLen < 0 {
			payloadLen = -payloadLen
		}
		if capHint > 4096 {
			capHint = 4096
		}
		if payloadLen > 2048 {
			payloadLen = 2048
		}

		dst := make([]byte, 0, capHint)
		out := ensureICMPBuf(dst, payloadLen)
		if len(out) != 8+payloadLen {
			t.Fatalf("len=%d want=%d", len(out), 8+payloadLen)
		}
		if cap(dst) >= 8+payloadLen && len(dst) == 0 && payloadLen > 0 {
			if &out[0] != &dst[:8+payloadLen][0] {
				t.Fatalf("expected reuse of dst backing array")
			}
		}
	})
}

// Verifies fillICMPEcho correctly constructs an ICMP echo request packet.
func FuzzUping_FillICMPEcho(f *testing.F) {
	f.Add(uint16(0x1234), uint16(7), []byte{1, 2, 3, 4, 5, 6, 7, 8})
	f.Fuzz(func(t *testing.T, id, seq uint16, payload []byte) {
		if len(payload) > 512 {
			payload = payload[:512]
		}
		dst := ensureICMPBuf(nil, len(payload))
		fillICMPEcho(dst, id, seq, payload)
		if dst[0] != 8 || dst[1] != 0 {
			t.Fatalf("not echo request")
		}
		if binary.BigEndian.Uint16(dst[4:6]) != id || binary.BigEndian.Uint16(dst[6:8]) != seq {
			t.Fatalf("id/seq mismatch")
		}
		if !bytes.Equal(dst[8:], payload) {
			t.Fatalf("payload mismatch")
		}
		if icmpChecksum(dst) != 0 {
			t.Fatalf("bad checksum")
		}
	})
}
