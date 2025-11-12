package liveness

import "testing"

func FuzzClient_Liveness_Packet_Unmarshal_NoPanic(f *testing.F) {
	f.Add(make([]byte, 40))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) < 40 {
			b = append(b, make([]byte, 40-len(b))...)
		}
		_, _ = UnmarshalControlPacket(b[:40])
	})
}
