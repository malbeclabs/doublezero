package geoprobe

import (
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

func TestICMPConn_RoundTrip(t *testing.T) {
	conn, err := newICMPConn(slog.Default())
	if err != nil {
		t.Skipf("skipping: need CAP_NET_RAW: %v", err)
	}
	defer conn.close()

	msg := &icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{ID: 0xBEEF, Seq: 1, Data: make([]byte, 56)},
	}
	payload, err := msg.Marshal(nil)
	require.NoError(t, err)

	_, err = conn.sendEcho(net.IPv4(127, 0, 0, 1), payload)
	require.NoError(t, err)

	require.NoError(t, conn.setReadDeadline(time.Now().Add(2*time.Second)))

	buf := make([]byte, 1500)
	n, rxTime, err := conn.recvEcho(buf)
	require.NoError(t, err)
	assert.Greater(t, n, 0)

	rtt := time.Since(rxTime)
	assert.GreaterOrEqual(t, rtt, time.Duration(0))
	assert.Less(t, rtt, 10*time.Millisecond)
}

func TestICMPConn_DeadlineExpired(t *testing.T) {
	conn, err := newICMPConn(slog.Default())
	if err != nil {
		t.Skipf("skipping: need CAP_NET_RAW: %v", err)
	}
	defer conn.close()

	require.NoError(t, conn.setReadDeadline(time.Now().Add(-1*time.Second)))

	buf := make([]byte, 1500)
	_, _, err = conn.recvEcho(buf)
	assert.Error(t, err)
}

func TestDecideRxTimestamp(t *testing.T) {
	now := time.Now()

	t.Run("normal kernel timestamp", func(t *testing.T) {
		kernel := now.Add(-10 * time.Microsecond)
		fallback := now
		result := decideRxTimestamp(kernel, fallback)
		assert.Equal(t, kernel, result)
	})

	t.Run("suspiciously early kernel timestamp", func(t *testing.T) {
		kernel := now.Add(-20 * time.Millisecond)
		fallback := now
		result := decideRxTimestamp(kernel, fallback)
		assert.Equal(t, fallback, result)
	})

	t.Run("kernel after fallback", func(t *testing.T) {
		kernel := now.Add(1 * time.Microsecond)
		fallback := now
		result := decideRxTimestamp(kernel, fallback)
		assert.Equal(t, kernel, result)
	})
}

func TestStripIPv4Header(t *testing.T) {
	tests := []struct {
		name  string
		buf   []byte
		wantN int
	}{
		{
			name:  "standard 20-byte header",
			buf:   append([]byte{0x45, 0, 0, 28, 0, 0, 0, 0, 64, 1, 0, 0, 127, 0, 0, 1, 127, 0, 0, 1}, []byte("icmpdata")...),
			wantN: 20,
		},
		{
			name:  "header with options (IHL=6, 24 bytes)",
			buf:   append(make([]byte, 24), []byte("icmp")...),
			wantN: 24,
		},
		{
			name:  "too short for minimum header",
			buf:   nil,
			wantN: 0,
		},
		{
			name:  "IHL too small",
			buf:   make([]byte, 20),
			wantN: 0,
		},
		{
			name:  "IHL larger than packet",
			buf:   append([]byte{0x49}, make([]byte, 19)...),
			wantN: 0,
		},
	}

	// Set IHL=6 for the "header with options" case.
	tests[1].buf[0] = 0x46

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := stripIPv4Header(tt.buf)
			assert.Equal(t, tt.wantN, n)
		})
	}
}
