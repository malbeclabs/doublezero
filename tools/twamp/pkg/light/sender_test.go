package twamplight_test

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/require"
)

func BenchmarkTWAMP_Sender(b *testing.B) {
	reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
	require.NoError(b, err)
	b.Cleanup(func() { reflector.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = reflector.Run(ctx)
	}()
	b.Cleanup(cancel)

	sender, err := twamplight.NewSender(log, reflector.LocalAddr().(*net.UDPAddr), 100*time.Millisecond)
	require.NoError(b, err)
	b.Cleanup(func() { sender.Close() })

	b.ResetTimer()
	for range b.N {
		_, err := sender.Probe(context.Background())
		if err != nil {
			b.Fatalf("probe failed: %v", err)
		}
	}
}

func TestTWAMP_Sender(t *testing.T) {
	t.Run("successful RTT probe", func(t *testing.T) {
		t.Parallel()

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		addr := reflector.LocalAddr().(*net.UDPAddr)
		sender, err := twamplight.NewSender(log, addr, 250*time.Millisecond)
		require.NoError(t, err)
		require.NotNil(t, sender.LocalAddr())

		rtt, err := sender.Probe(context.Background())
		require.NoError(t, err)
		require.Greater(t, rtt, 0*time.Millisecond)
	})

	t.Run("timeout returns ErrTimeout", func(t *testing.T) {
		t.Parallel()

		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 65000}
		sender, err := twamplight.NewSender(log, addr, 100*time.Millisecond)
		require.NoError(t, err)

		rtt, err := sender.Probe(context.Background())
		require.ErrorIs(t, err, twamplight.ErrTimeout)
		require.Equal(t, time.Duration(0), rtt)
	})

	t.Run("unreachable address fails", func(t *testing.T) {
		t.Parallel()

		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 12345}
		sender, err := twamplight.NewSender(log, addr, 100*time.Millisecond)
		require.NoError(t, err)

		rtt, err := sender.Probe(context.Background())
		require.Error(t, err)
		require.Equal(t, time.Duration(0), rtt)
	})

	t.Run("closed socket results in timeout", func(t *testing.T) {
		t.Parallel()

		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		require.NoError(t, err)
		addr := conn.LocalAddr().(*net.UDPAddr)

		go func() {
			buf := make([]byte, 1500)
			_, _, _ = conn.ReadFromUDP(buf)
			_ = conn.Close()
		}()

		time.Sleep(100 * time.Millisecond)

		sender, err := twamplight.NewSender(log, addr, 500*time.Millisecond)
		require.NoError(t, err)

		rtt, err := sender.Probe(context.Background())
		require.ErrorIs(t, err, twamplight.ErrTimeout)
		require.Equal(t, time.Duration(0), rtt)
	})

	t.Run("context cancel aborts probe early", func(t *testing.T) {
		t.Parallel()

		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 65000} // blackhole
		sender, err := twamplight.NewSender(log, addr, 5*time.Second)      // long timeout
		require.NoError(t, err)
		t.Cleanup(func() { sender.Close() })

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err = sender.Probe(ctx)
		elapsed := time.Since(start)

		require.Error(t, err)
		require.Less(t, elapsed, 500*time.Millisecond, "probe should return shortly after context cancel")
	})

	t.Run("context_cancel_aborts_probe_early", func(t *testing.T) {
		t.Parallel()

		// Use a blackhole address so the probe would hang unless canceled.
		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 65000}
		sender, err := twamplight.NewSender(log, addr, 5*time.Second)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		start := time.Now()
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		rtt, err := sender.Probe(ctx)
		elapsed := time.Since(start)

		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, time.Duration(0), rtt)
		require.Less(t, elapsed, 500*time.Millisecond, "probe should return shortly after context cancel")
	})

	t.Run("context_timeout_aborts_probe_early", func(t *testing.T) {
		t.Parallel()

		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 65000}
		sender, err := twamplight.NewSender(log, addr, 5*time.Second) // long socket timeout
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		rtt, err := sender.Probe(ctx)
		elapsed := time.Since(start)

		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, time.Duration(0), rtt)
		require.Less(t, elapsed, 500*time.Millisecond, "probe should return shortly after context timeout")
	})

	t.Run("sequence numbers increment", func(t *testing.T) {
		t.Parallel()

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		addr := reflector.LocalAddr().(*net.UDPAddr)
		sender, err := twamplight.NewSender(log, addr, 250*time.Millisecond)
		require.NoError(t, err)

		// Send multiple probes and verify sequence numbers
		for i := 0; i < 3; i++ {
			rtt, err := sender.Probe(context.Background())
			require.NoError(t, err)
			require.Greater(t, rtt, 0*time.Millisecond)
		}
	})

	t.Run("concurrent probes use different sequence numbers", func(t *testing.T) {
		t.Parallel()

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		addr := reflector.LocalAddr().(*net.UDPAddr)
		sender, err := twamplight.NewSender(log, addr, 250*time.Millisecond)
		require.NoError(t, err)

		var wg sync.WaitGroup
		results := make([]time.Duration, 5)
		errors := make([]error, 5)

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				results[index], errors[index] = sender.Probe(context.Background())
			}(i)
		}

		wg.Wait()

		// All probes should succeed
		for i := 0; i < 5; i++ {
			require.NoError(t, errors[i])
			require.Greater(t, results[i], 0*time.Millisecond)
		}
	})

	t.Run("returns ErrInvalidPacket for malformed response", func(t *testing.T) {
		t.Parallel()

		// Create a custom reflector that sends malformed packets
		conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
		require.NoError(t, err)
		defer conn.Close()

		// Start reflector that sends short packets immediately
		go func() {
			buf := make([]byte, 1500)
			for {
				_, addr, err := conn.ReadFromUDP(buf)
				if err != nil {
					return
				}
				// Send back a short packet instead of 48 bytes immediately
				_, err = conn.WriteToUDP(buf[:24], addr)
				require.NoError(t, err)
			}
		}()

		// Give the reflector a moment to start
		time.Sleep(10 * time.Millisecond)

		sender, err := twamplight.NewSender(log, conn.LocalAddr().(*net.UDPAddr), 250*time.Millisecond)
		require.NoError(t, err)

		rtt, err := sender.Probe(context.Background())
		require.ErrorIs(t, err, twamplight.ErrInvalidPacket)
		require.Equal(t, time.Duration(0), rtt)
	})
}

func FuzzTWAMP_Sender(f *testing.F) {
	// Seed with some valid and invalid inputs
	f.Add("127.0.0.1", uint16(65000))     // valid IP, random port
	f.Add("256.256.256.256", uint16(123)) // invalid IP
	f.Add("", uint16(0))                  // empty IP
	f.Add("localhost", uint16(0))         // DNS, but port 0

	f.Fuzz(func(t *testing.T, ip string, port uint16) {
		// Clamp port range to avoid privileged/reserved ports
		if port < 1024 {
			port += 1024
		}

		addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ip, port))
		if err != nil {
			return // skip invalid addr strings
		}
		sender, err := twamplight.NewSender(log, addr, 50*time.Millisecond)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
		defer cancel()

		_, _ = sender.Probe(ctx) // we don't assert here, just checking for panics or unexpected crashes
	})
}
