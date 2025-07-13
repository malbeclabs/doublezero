package twamplight_test

import (
	"context"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/require"
)

func TestTWAMP_Reflector_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	runReflectorTests(t, func(addr string) (twamplight.Reflector, error) {
		return twamplight.NewLinuxReflector(addr, 100*time.Millisecond)
	})
}

func TestTWAMP_Reflector_Basic(t *testing.T) {
	runReflectorTests(t, func(addr string) (twamplight.Reflector, error) {
		return twamplight.NewBasicReflector(log, addr, 100*time.Millisecond)
	})
}

func runReflectorTests(t *testing.T, newReflector func(addr string) (twamplight.Reflector, error)) {
	t.Run("echo", func(t *testing.T) {
		t.Parallel()

		reflector, err := newReflector("127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
		require.NoError(t, err)
		defer conn.Close()

		// Create valid packet.
		payload := make([]byte, twamplight.PacketSize)
		payload[0] = 1 // sequence number
		payload[4] = 1 // timestamp
		// padding remains zeros (default)
		_, err = conn.Write(payload)
		require.NoError(t, err)

		buf := make([]byte, 1500)
		n, err := conn.Read(buf)
		require.NoError(t, err)
		require.Equal(t, twamplight.PacketSize, n)
		require.Equal(t, payload, buf[:n])
	})

	t.Run("graceful shutdown", func(t *testing.T) {
		t.Parallel()

		reflector, err := newReflector("127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			require.NoError(t, reflector.Run(ctx))
		}()

		time.Sleep(100 * time.Millisecond)
		cancel()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("reflector did not exit after context cancellation")
		}

		require.NoError(t, reflector.Close()) // idempotency check
		require.NoError(t, reflector.Close())
	})

	t.Run("truncates oversized packet", func(t *testing.T) {
		t.Parallel()

		reflector, err := newReflector("127.0.0.1:0")
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
		require.NoError(t, err)
		defer conn.Close()

		payload := make([]byte, 2000)
		for i := range payload {
			payload[i] = byte(i % 256)
		}
		_, err = conn.Write(payload)
		require.NoError(t, err)

		// Should not receive a response for oversized packet
		err = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		require.NoError(t, err)

		buf := make([]byte, 1500)
		_, err = conn.Read(buf)
		require.Error(t, err, "should not receive response for oversized packet")
	})

	t.Run("concurrent clients", func(t *testing.T) {
		t.Parallel()

		reflector, err := newReflector("127.0.0.1:0")
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		addr := reflector.LocalAddr()

		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				conn, err := net.DialUDP("udp", nil, addr)
				require.NoError(t, err)
				defer conn.Close()

				// Create valid packet.
				msg := make([]byte, twamplight.PacketSize)
				msg[0] = byte(i) // sequence number
				msg[4] = 1       // timestamp
				// padding remains zeros (default)
				_, err = conn.Write(msg)
				require.NoError(t, err)

				buf := make([]byte, 1500)
				n, err := conn.Read(buf)
				require.NoError(t, err)
				require.Equal(t, twamplight.PacketSize, n)
				require.Equal(t, msg, buf[:n])
			}(i)
		}
		wg.Wait()
	})

	t.Run("rejects oversized packets", func(t *testing.T) {
		t.Parallel()

		reflector, err := newReflector("127.0.0.1:0")
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
		require.NoError(t, err)
		defer conn.Close()

		// Send oversized packet
		payload := make([]byte, 100)
		_, err = conn.Write(payload)
		require.NoError(t, err)

		// Should not receive a response
		err = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		require.NoError(t, err)

		buf := make([]byte, 1500)
		_, err = conn.Read(buf)
		require.Error(t, err, "should not receive response for oversized packet")
	})

	t.Run("rejects undersized packets", func(t *testing.T) {
		t.Parallel()

		reflector, err := newReflector("127.0.0.1:0")
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
		require.NoError(t, err)
		defer conn.Close()

		// Send undersized packet
		payload := make([]byte, 24) // half size
		_, err = conn.Write(payload)
		require.NoError(t, err)

		// Should not receive a response
		err = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		require.NoError(t, err)

		buf := make([]byte, 1500)
		_, err = conn.Read(buf)
		require.Error(t, err, "should not receive response for undersized packet")
	})

	t.Run("rejects packets with non-zero padding", func(t *testing.T) {
		t.Parallel()

		reflector, err := newReflector("127.0.0.1:0")
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
		require.NoError(t, err)
		defer conn.Close()

		// Create packet with non-zero padding
		payload := make([]byte, twamplight.PacketSize)
		payload[0] = 1  // sequence number
		payload[4] = 1  // timestamp
		payload[20] = 1 // non-zero padding
		_, err = conn.Write(payload)
		require.NoError(t, err)

		// Should not receive a response
		err = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		require.NoError(t, err)

		buf := make([]byte, 1500)
		_, err = conn.Read(buf)
		require.Error(t, err, "should not receive response for packet with non-zero padding")
	})

	t.Run("accepts valid packets", func(t *testing.T) {
		t.Parallel()

		reflector, err := newReflector("127.0.0.1:0")
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
		require.NoError(t, err)
		defer conn.Close()

		// Create valid packet
		payload := make([]byte, twamplight.PacketSize)
		payload[0] = 1 // sequence number
		payload[4] = 1 // timestamp
		// padding remains zeros (default)
		_, err = conn.Write(payload)
		require.NoError(t, err)

		// Should receive the packet back
		err = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		require.NoError(t, err)

		buf := make([]byte, 1500)
		n, err := conn.Read(buf)
		require.NoError(t, err)
		require.Equal(t, twamplight.PacketSize, n)
		require.Equal(t, payload, buf[:n])
	})
}
