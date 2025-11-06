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

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer conn.Close()

	rx := NewReceiver(slog.Default(), conn, func(*ControlPacket, Peer) {})
	done := make(chan struct{})
	go func() { rx.Run(ctx); close(done) }()

	// Cancel and ensure the loop exits
	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("receiver did not exit after cancel")
	}
}

func TestClient_Liveness_Receiver_IgnoresMalformedPacket(t *testing.T) {
	t.Parallel()

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer conn.Close()

	var calls int32
	rx := NewReceiver(slog.Default(), conn, func(*ControlPacket, Peer) {
		atomic.AddInt32(&calls, 1)
	})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { rx.Run(ctx); close(done) }()

	// Send a short (<40) payload to trigger UnmarshalControlPacket "short" error.
	cl, err := net.DialUDP("udp4", nil, conn.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	_, err = cl.Write(make([]byte, 20))
	require.NoError(t, err)
	_ = cl.Close()

	// Give the receiver a moment to read & ignore it, then cancel.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("receiver did not exit after cancel")
	}

	require.Equal(t, int32(0), atomic.LoadInt32(&calls), "handler should not be called for malformed packet")
}
