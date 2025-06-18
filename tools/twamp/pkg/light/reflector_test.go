package twamplight_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/require"
)

func TestTWAMP_Reflector(t *testing.T) {
	t.Run("echo", func(t *testing.T) {
		t.Parallel()

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr().(*net.UDPAddr))
		require.NoError(t, err)
		defer conn.Close()

		// Create valid 48-byte TWAMP packet
		payload := make([]byte, 48)
		payload[0] = 1 // sequence number
		payload[4] = 1 // timestamp
		// padding remains zeros (default)
		_, err = conn.Write(payload)
		require.NoError(t, err)

		buf := make([]byte, 1500)
		n, err := conn.Read(buf)
		require.NoError(t, err)
		require.Equal(t, 48, n)
		require.Equal(t, payload, buf[:n])
	})

	t.Run("graceful shutdown", func(t *testing.T) {
		t.Parallel()

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(context.Background())
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

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr().(*net.UDPAddr))
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
		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		addr := reflector.LocalAddr().(*net.UDPAddr)

		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				conn, err := net.DialUDP("udp", nil, addr)
				require.NoError(t, err)
				defer conn.Close()

				// Create valid 48-byte TWAMP packet
				msg := make([]byte, 48)
				msg[0] = byte(i) // sequence number
				msg[4] = 1       // timestamp
				// padding remains zeros (default)
				_, err = conn.Write(msg)
				require.NoError(t, err)

				buf := make([]byte, 1500)
				n, err := conn.Read(buf)
				require.NoError(t, err)
				require.Equal(t, 48, n)
				require.Equal(t, msg, buf[:n])
			}(i)
		}
		wg.Wait()
	})

	t.Run("rejects oversized packets", func(t *testing.T) {
		t.Parallel()

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr().(*net.UDPAddr))
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

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr().(*net.UDPAddr))
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

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr().(*net.UDPAddr))
		require.NoError(t, err)
		defer conn.Close()

		// Create 48-byte packet with non-zero padding
		payload := make([]byte, 48)
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

	t.Run("accepts valid TWAMP packets", func(t *testing.T) {
		t.Parallel()

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr().(*net.UDPAddr))
		require.NoError(t, err)
		defer conn.Close()

		// Create valid 48-byte TWAMP packet
		payload := make([]byte, 48)
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
		require.Equal(t, 48, n)
		require.Equal(t, payload, buf[:n])
	})
}

func FuzzTWAMP_Reflector(f *testing.F) {
	reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
	require.NoError(f, err)
	defer reflector.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = reflector.Run(ctx)
	}()
	defer cancel()

	addr := reflector.LocalAddr().(*net.UDPAddr)

	conn, err := net.DialUDP("udp", nil, addr)
	require.NoError(f, err)
	defer conn.Close()

	// Seed with a basic valid TWAMP packet
	f.Add([]byte{
		0, 0, 0, 1, // seq
		0, 0, 0, 0, 0, 0, 0, 0, // timestamp
	})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 || len(data) > 1500 {
			return
		}
		_, _ = conn.Write(data)

		// Expect a response or timeout
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		buf := make([]byte, 1500)
		_, _, _ = conn.ReadFrom(buf) // we don't assert here — we just want to ensure no panics/crashes
	})
}
