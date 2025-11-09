package liveness

import (
	"context"
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

	udp, err := ListenUDP("127.0.0.1", 0)
	require.NoError(t, err)
	defer udp.Close()

	rx := NewReceiver(newTestLogger(t), udp, func(*ControlPacket, Peer) {})

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
	_ = udp.Close()

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

	udp, err := ListenUDP("127.0.0.1", 0)
	require.NoError(t, err)
	defer udp.Close()

	var calls int32
	rx := NewReceiver(newTestLogger(t), udp, func(*ControlPacket, Peer) {
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
	cl, err := net.DialUDP("udp4", nil, udp.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	_, err = cl.Write(make([]byte, 20))
	require.NoError(t, err)
	_ = cl.Close()

	time.Sleep(25 * time.Millisecond) // tiny nudge

	// Cancel, then close socket to force immediate unblock
	cancel()
	_ = udp.Close()

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

func TestClient_Liveness_Receiver_HandlerInvoked_WithPeerContext(t *testing.T) {
	t.Parallel()
	udp, err := ListenUDP("127.0.0.1", 0)
	require.NoError(t, err)
	defer udp.Close()

	var got Peer
	calls := int32(0)
	rx := NewReceiver(newTestLogger(t), udp, func(cp *ControlPacket, p Peer) { got = p; atomic.AddInt32(&calls, 1) })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { require.NoError(t, rx.Run(ctx)); close(done) }()

	// send a valid control packet
	cl, err := net.DialUDP("udp4", nil, udp.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	defer cl.Close()
	pkt := (&ControlPacket{Version: 1, State: StateInit, DetectMult: 1, Length: 40}).Marshal()
	_, err = cl.Write(pkt)
	require.NoError(t, err)

	require.Eventually(t, func() bool { return atomic.LoadInt32(&calls) == 1 }, time.Second, 10*time.Millisecond)
	require.NotEmpty(t, got.Interface) // usually lo/lo0
	require.Equal(t, "127.0.0.1", got.LocalIP)
	require.Equal(t, "127.0.0.1", got.RemoteIP)

	cancel()
	_ = udp.Close()
	<-done
}

func TestClient_Liveness_Receiver_DeadlineTimeoutsAreSilent(t *testing.T) {
	t.Parallel()
	udp, err := ListenUDP("127.0.0.1", 0)
	require.NoError(t, err)
	defer udp.Close()

	rx := NewReceiver(newTestLogger(t), udp, func(*ControlPacket, Peer) {})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { require.NoError(t, rx.Run(ctx)); close(done) }()

	// no traffic; ensure loop keeps running past a few deadlines
	time.Sleep(600 * time.Millisecond)
	cancel()
	_ = udp.Close()
	<-done
}

func TestClient_Liveness_Receiver_SocketClosed_ReturnsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	udp, err := ListenUDP("127.0.0.1", 0)
	require.NoError(t, err)

	rx := NewReceiver(newTestLogger(t), udp, func(*ControlPacket, Peer) {})
	errCh := make(chan error, 1)
	go func() { errCh <- rx.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	_ = udp.Close()
	err = <-errCh
	require.Error(t, err)
	require.Contains(t, err.Error(), "socket closed")
}
