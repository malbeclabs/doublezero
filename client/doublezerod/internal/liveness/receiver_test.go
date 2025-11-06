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

	fatalCh := make(chan error, 1)
	rx := NewReceiver(slog.Default(), conn, func(*ControlPacket, Peer) {}, fatalCh)

	done := make(chan struct{})
	go func() { rx.Run(ctx); close(done) }()

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
	fatalCh := make(chan error, 1)
	rx := NewReceiver(slog.Default(), conn, func(*ControlPacket, Peer) {
		atomic.AddInt32(&calls, 1)
	}, fatalCh)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { rx.Run(ctx); close(done) }()

	// Ensure the loop is alive: send a short (<40) payload to trigger parse error.
	cl, err := net.DialUDP("udp4", nil, conn.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	_, err = cl.Write(make([]byte, 20))
	require.NoError(t, err)
	_ = cl.Close()

	// Give it a beat to read & ignore, then cancel and close to unblock read.
	time.Sleep(50 * time.Millisecond)
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

	require.Equal(t, int32(0), atomic.LoadInt32(&calls), "handler should not be called for malformed packet")
}
