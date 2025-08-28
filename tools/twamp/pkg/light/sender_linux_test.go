package twamplight

import (
	"testing"
	"time"
)

func TestDecideRTT(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	cases := []struct {
		name                       string
		send, recvKernel, recvUser time.Time
		want                       time.Duration
	}{
		{
			name:       "normal positive RTT uses kernel",
			send:       now,
			recvKernel: now.Add(500 * time.Microsecond),
			recvUser:   now.Add(600 * time.Microsecond),
			want:       500 * time.Microsecond,
		},
		{
			name:       "kernel timestamp slightly before send → clamp to 0",
			send:       now,
			recvKernel: now.Add(-50 * time.Microsecond), // kernel earlier
			recvUser:   now.Add(100 * time.Microsecond),
			want:       0,
		},
		{
			name:       "kernel timestamp way before send → fallback to userspace recv",
			send:       now,
			recvKernel: now.Add(-500 * time.Microsecond), // way earlier
			recvUser:   now.Add(200 * time.Microsecond),
			want:       200 * time.Microsecond,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := decideRTT(c.send, c.recvKernel, c.recvUser)
			if got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
