package latency

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
)

func TestClient_Latency_UDPPing(t *testing.T) {
	t.Run("NoRouteOrBlackhole_ReturnsWithinDeadline", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		start := time.Now()
		_ = udpPing(ctx, newTestLogger(t), newTestDevice("192.0.2.1"))
		elapsed := time.Since(start)

		assert.Less(t, elapsed, 900*time.Millisecond)
	})

	t.Run("CancelImmediately_StopsPromptly", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		start := time.Now()
		_ = udpPing(ctx, newTestLogger(t), newTestDevice("127.0.0.1"))
		elapsed := time.Since(start)

		assert.Less(t, elapsed, 200*time.Millisecond, "udpPing should return quickly after ctx cancel")
	})
}

func newTestDevice(ip string) serviceability.Device {
	var d serviceability.Device
	b := net.ParseIP(ip).To16()
	copy(d.PublicIp[:], b)
	return d
}
