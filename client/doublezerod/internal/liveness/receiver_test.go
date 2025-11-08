package liveness

import (
	"context"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClient_Liveness_Receiver_CancelStopsLoop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	conn, err := ListenUDP("127.0.0.1", 0)
	require.NoError(t, err)
	defer conn.Close()

	rx := NewReceiver(slog.Default(), conn, func(*ControlPacket, Peer) {})

	done := make(chan struct{})
	go func() {
		err := rx.Run(ctx)
		require.NoError(t, err)
		close(done)
	}()

	// Nudge the loop to ensure it has started by forcing one deadline cycle.
	time.Sleep(50 * time.Millisecond)

	// Cancel and close to unblock any in-flight ReadFrom immediately.
	cancel()
	_ = conn.Close()

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 3*time.Second, 25*time.Millisecond, "receiver did not exit after cancel+close")
}

func TestClient_Liveness_Receiver_IgnoresMalformedPacket(t *testing.T) {
	t.Parallel()

	conn, err := ListenUDP("127.0.0.1", 0)
	require.NoError(t, err)
	defer conn.Close()

	var calls int32
	rx := NewReceiver(slog.Default(), conn, func(*ControlPacket, Peer) {
		atomic.AddInt32(&calls, 1)
	})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		err := rx.Run(ctx)
		require.NoError(t, err)
		close(done)
	}()

	// Ensure loop is running: send malformed (<40 bytes)
	cl, err := net.DialUDP("udp4", nil, conn.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	_, err = cl.Write(make([]byte, 20))
	require.NoError(t, err)
	_ = cl.Close()

	time.Sleep(25 * time.Millisecond) // tiny nudge

	// Cancel, then close socket to force immediate unblock
	cancel()
	_ = conn.Close()

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 5*time.Second, 100*time.Millisecond, "receiver did not exit after cancel+close")

	require.Equal(t, int32(0), atomic.LoadInt32(&calls))
}
