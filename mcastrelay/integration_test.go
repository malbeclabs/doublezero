package mcastrelay_test

import (
	"context"
	"log/slog"
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/mcastrelay/internal/multicast"
	"github.com/malbeclabs/doublezero/mcastrelay/internal/server"
	pb "github.com/malbeclabs/doublezero/mcastrelay/proto/relay/gen/pb-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestIntegration_MulticastLoopback tests that the multicast listener can receive
// packets sent to itself when loopback is enabled. This exercises the full UDP
// receive path.
func TestIntegration_MulticastLoopback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multicast integration test in short mode")
	}

	// Use a random port to avoid conflicts with other tests
	port := 10000 + rand.Intn(5000)
	multicastIP := "239.255.255.250" // Link-local multicast address

	cfg := &multicast.Config{
		Logger:            slog.Default(),
		MulticastIP:       multicastIP,
		Port:              port,
		BufferSize:        65535,
		ReadTimeout:       100 * time.Millisecond,
		MulticastLoopback: true,
	}

	listener, err := multicast.NewListener(cfg)
	require.NoError(t, err)

	// Subscribe to receive packets
	packets := make(chan multicast.Packet, 10)
	unsubscribe := listener.Subscribe(packets)
	defer unsubscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start listener in background
	listenerErr := make(chan error, 1)
	go func() {
		listenerErr <- listener.Run(ctx)
	}()

	// Wait for listener to start
	time.Sleep(100 * time.Millisecond)

	// Send a multicast packet to ourselves
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{
		IP:   net.ParseIP(multicastIP),
		Port: port,
	})
	require.NoError(t, err)
	defer conn.Close()

	testData := []byte("hello multicast loopback")
	n, err := conn.Write(testData)
	require.NoError(t, err)
	require.Equal(t, len(testData), n)

	// Verify we receive the packet
	select {
	case pkt := <-packets:
		assert.Equal(t, testData, pkt.Data)
		assert.WithinDuration(t, time.Now(), pkt.ReceivedAt, time.Second)
		t.Logf("received packet: %s", string(pkt.Data))
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive multicast packet within timeout")
	}

	// Cleanup
	cancel()
	select {
	case err := <-listenerErr:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("listener did not shut down")
	}
}

// TestIntegration_MulticastToGRPC tests the complete end-to-end path:
// UDP multicast packet -> Listener -> gRPC Server -> gRPC Client
func TestIntegration_MulticastToGRPC(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multicast integration test in short mode")
	}

	// Use random ports to avoid conflicts
	mcastPort := 10000 + rand.Intn(5000)
	multicastIP := "239.255.255.250"

	// Create multicast listener with loopback enabled
	mcastCfg := &multicast.Config{
		Logger:            slog.Default(),
		MulticastIP:       multicastIP,
		Port:              mcastPort,
		BufferSize:        65535,
		ReadTimeout:       100 * time.Millisecond,
		MulticastLoopback: true,
	}

	mcastListener, err := multicast.NewListener(mcastCfg)
	require.NoError(t, err)

	// Create gRPC listener on random port
	grpcLis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// Create gRPC server
	srv, err := server.New(&server.Config{
		Logger:        slog.Default(),
		Listener:      mcastListener,
		ChannelBuffer: 256,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start multicast listener
	mcastErr := make(chan error, 1)
	go func() {
		mcastErr <- mcastListener.Run(ctx)
	}()

	// Start gRPC server
	grpcErr := make(chan error, 1)
	go func() {
		grpcErr <- srv.Serve(grpcLis)
	}()
	defer srv.Stop()

	// Wait for services to start
	time.Sleep(100 * time.Millisecond)

	// Create gRPC client
	conn, err := grpc.NewClient(
		grpcLis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewRelayServiceClient(conn)

	// Subscribe to the stream
	streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
	defer streamCancel()

	stream, err := client.Subscribe(streamCtx, &pb.SubscribeRequest{})
	require.NoError(t, err)

	// Wait for subscription to be established
	time.Sleep(100 * time.Millisecond)

	// Send a multicast packet
	mcastConn, err := net.DialUDP("udp4", nil, &net.UDPAddr{
		IP:   net.ParseIP(multicastIP),
		Port: mcastPort,
	})
	require.NoError(t, err)
	defer mcastConn.Close()

	testData := []byte("end-to-end integration test payload")
	n, err := mcastConn.Write(testData)
	require.NoError(t, err)
	require.Equal(t, len(testData), n)

	// Receive via gRPC stream
	msg, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, testData, msg.Payload)
	assert.NotNil(t, msg.ReceivedAt)
	t.Logf("received via gRPC: %s", string(msg.Payload))
}

// TestIntegration_MultiplePackets tests receiving multiple packets in sequence.
func TestIntegration_MultiplePackets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multicast integration test in short mode")
	}

	port := 10000 + rand.Intn(5000)
	multicastIP := "239.255.255.250"

	cfg := &multicast.Config{
		Logger:            slog.Default(),
		MulticastIP:       multicastIP,
		Port:              port,
		BufferSize:        65535,
		ReadTimeout:       100 * time.Millisecond,
		MulticastLoopback: true,
	}

	listener, err := multicast.NewListener(cfg)
	require.NoError(t, err)

	packets := make(chan multicast.Packet, 100)
	unsubscribe := listener.Subscribe(packets)
	defer unsubscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = listener.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Create sender
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{
		IP:   net.ParseIP(multicastIP),
		Port: port,
	})
	require.NoError(t, err)
	defer conn.Close()

	// Send multiple packets
	const numPackets = 10
	for i := 0; i < numPackets; i++ {
		data := []byte("packet-" + string(rune('A'+i)))
		_, err := conn.Write(data)
		require.NoError(t, err)
		// Small delay between packets
		time.Sleep(10 * time.Millisecond)
	}

	// Receive all packets
	received := 0
	timeout := time.After(5 * time.Second)

	for received < numPackets {
		select {
		case pkt := <-packets:
			t.Logf("received packet %d: %s", received+1, string(pkt.Data))
			received++
		case <-timeout:
			t.Fatalf("only received %d/%d packets", received, numPackets)
		}
	}

	assert.Equal(t, numPackets, received)
}
